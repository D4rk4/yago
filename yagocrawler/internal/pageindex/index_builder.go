package pageindex

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

// maxExtractedTextBytes bounds the extracted page text the crawler ships, so a
// pathological page cannot send an unbounded body over the wire. It matches the
// node document store's own bound, so this only trims text the node would discard
// anyway while keeping the crawler's memory and payload bounded.
const maxExtractedTextBytes = 1 << 20

type Artifacts struct {
	Postings []yagomodel.RWIPosting
	Metadata yagomodel.URIMetadataRow
	Document yagocrawlcontract.DocumentIngest
}

type IndexBuilder interface {
	Build(page pageparse.ParsedPage, stats pageparse.PageStats) (Artifacts, error)
}

type contentIndexBuilder struct {
	clock func() time.Time
}

func NewIndexBuilder() IndexBuilder {
	return &contentIndexBuilder{clock: time.Now}
}

func (b *contentIndexBuilder) Build(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
) (Artifacts, error) {
	indexedAt := b.clock()
	postings := BuildPostings(page, stats)
	metadata := BuildMetadata(page, stats, indexedAt)
	document := BuildDocument(page, stats, metadata, indexedAt)
	return Artifacts{Postings: postings, Metadata: metadata, Document: document}, nil
}

func BuildDocument(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
	metadata yagomodel.URIMetadataRow,
	indexedAt time.Time,
) yagocrawlcontract.DocumentIngest {
	hash := sha256.Sum256([]byte(page.Text))
	outlinks := make([]string, 0, len(stats.LocalLinks)+len(stats.ExternalLinks))
	outlinks = append(outlinks, stats.LocalLinks...)
	outlinks = append(outlinks, stats.ExternalLinks...)

	return yagocrawlcontract.DocumentIngest{
		CanonicalURL:  documentCanonicalURL(page),
		NormalizedURL: page.URL,
		Title:         page.Title,
		Headings:      append([]string(nil), page.Headings...),
		ExtractedText: boundedText(page.Text),
		Language:      NormalizeLanguage(page.Language),
		FetchStatus:   "fetched",
		IndexedAt:     indexedAt.UTC(),
		ContentHash:   hex.EncodeToString(hash[:]),
		Outlinks:      outlinks,
		Images:        imageMetadataFromPage(page.Images),
		Metadata:      documentMetadata(page, metadata),
	}
}

// boundedText truncates text to maxExtractedTextBytes on a UTF-8 rune boundary,
// so a partial rune is never emitted. Text within the bound is returned unchanged.
func boundedText(text string) string {
	if len(text) <= maxExtractedTextBytes {
		return text
	}
	end := maxExtractedTextBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}

	return text[:end]
}

func imageMetadataFromPage(in []pageparse.ImageMetadata) []yagocrawlcontract.ImageMetadata {
	out := make([]yagocrawlcontract.ImageMetadata, 0, len(in))
	for _, image := range in {
		out = append(out, yagocrawlcontract.ImageMetadata{
			URL:     image.URL,
			AltText: image.AltText,
		})
	}

	return out
}

func documentCanonicalURL(page pageparse.ParsedPage) string {
	if page.CanonicalURL != "" {
		return page.CanonicalURL
	}
	return page.URL
}

func documentMetadata(
	page pageparse.ParsedPage,
	metadata yagomodel.URIMetadataRow,
) map[string]string {
	values := map[string]string{"url_hash": metadata.Properties[yagomodel.URLMetaHash]}
	if page.Description != "" {
		values["description"] = page.Description
	}
	return values
}
