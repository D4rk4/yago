package pageindex

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
	"github.com/D4rk4/yago/yacymodel"
)

type Artifacts struct {
	Postings []yacymodel.RWIPosting
	Metadata yacymodel.URIMetadataRow
	Document yacycrawlcontract.DocumentIngest
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
	metadata yacymodel.URIMetadataRow,
	indexedAt time.Time,
) yacycrawlcontract.DocumentIngest {
	hash := sha256.Sum256([]byte(page.Text))
	outlinks := make([]string, 0, len(stats.LocalLinks)+len(stats.ExternalLinks))
	outlinks = append(outlinks, stats.LocalLinks...)
	outlinks = append(outlinks, stats.ExternalLinks...)

	return yacycrawlcontract.DocumentIngest{
		CanonicalURL:  documentCanonicalURL(page),
		NormalizedURL: page.URL,
		Title:         page.Title,
		Headings:      append([]string(nil), page.Headings...),
		ExtractedText: page.Text,
		Language:      NormalizeLanguage(page.Language),
		FetchStatus:   "fetched",
		IndexedAt:     indexedAt.UTC(),
		ContentHash:   hex.EncodeToString(hash[:]),
		Outlinks:      outlinks,
		Metadata: map[string]string{
			"url_hash": metadata.Properties[yacymodel.URLMetaHash],
		},
	}
}

func documentCanonicalURL(page pageparse.ParsedPage) string {
	if page.CanonicalURL != "" {
		return page.CanonicalURL
	}
	return page.URL
}
