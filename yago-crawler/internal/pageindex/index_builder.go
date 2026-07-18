package pageindex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

const maxExtractedTextBytes = yagocrawlcontract.MaximumDocumentTextBytes

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
	if len(page.URL) > yagocrawlcontract.MaximumCrawlURLBytes {
		return Artifacts{}, fmt.Errorf(
			"page URL exceeds %d bytes",
			yagocrawlcontract.MaximumCrawlURLBytes,
		)
	}
	indexedAt := b.clock()
	page.Language = resolveContentLanguage(page.Text, page.Language)
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
		Title:         boundedTextBytes(page.Title, yagocrawlcontract.MaximumDocumentTitleBytes),
		Headings: boundedStrings(
			page.Headings,
			yagocrawlcontract.MaximumDocumentHeadings,
			yagocrawlcontract.MaximumDocumentHeadingBytes,
		),
		ExtractedText:  boundedText(page.Text),
		Language:       NormalizeLanguage(page.Language),
		FetchStatus:    "fetched",
		IndexedAt:      indexedAt.UTC(),
		PublishedAt:    page.PublishedAt.UTC(),
		ModifiedAt:     page.ModifiedAt.UTC(),
		DateConfidence: page.DateConfidence,
		DateSource:     page.DateSource,
		ContentHash:    hex.EncodeToString(hash[:]),
		Outlinks: boundedURLs(
			outlinks,
			yagocrawlcontract.MaximumDocumentOutlinks,
		),
		OutboundAnchors: outboundAnchorsFromPage(
			page.OutboundAnchors,
		),
		OutboundAnchorEvidenceKnown: true,
		SafetyLabels:                safetyLabelsFromPage(page.SafetyLabels),
		Images:                      imageMetadataFromPage(page.Images),
		Metadata:                    documentMetadata(page, metadata),
	}
}

func safetyLabelsFromPage(in pageparse.SafetyLabels) yagocrawlcontract.SafetyLabels {
	var familyFriendly *bool
	if in.FamilyFriendly != nil {
		value := *in.FamilyFriendly
		familyFriendly = &value
	}

	return yagocrawlcontract.SafetyLabels{
		RatingValues:   append([]string(nil), in.RatingValues...),
		FamilyFriendly: familyFriendly,
	}
}

func outboundAnchorsFromPage(
	in []pageparse.OutboundAnchor,
) []yagocrawlcontract.OutboundAnchor {
	out := make(
		[]yagocrawlcontract.OutboundAnchor,
		0,
		min(len(in), yagocrawlcontract.MaximumDocumentAnchors),
	)
	for _, anchor := range in {
		if len(anchor.TargetURL) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		out = append(out, yagocrawlcontract.OutboundAnchor{
			TargetURL: anchor.TargetURL,
			Text: boundedTextBytes(
				anchor.Text,
				yagocrawlcontract.MaximumDocumentMetadataBytes,
			),
			NoFollow:      anchor.NoFollow,
			UserGenerated: anchor.UserGenerated,
			Sponsored:     anchor.Sponsored,
		})
		if len(out) == yagocrawlcontract.MaximumDocumentAnchors {
			break
		}
	}

	return out
}

// boundedText truncates text to maxExtractedTextBytes on a UTF-8 rune boundary,
// so a partial rune is never emitted. Text within the bound is returned unchanged.
func boundedText(text string) string {
	return boundedTextBytes(text, maxExtractedTextBytes)
}

func boundedTextBytes(text string, maximum int) string {
	if len(text) <= maximum {
		return text
	}
	end := maximum
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}

	return text[:end]
}

func boundedStrings(values []string, maximum, maximumBytes int) []string {
	values = values[:min(len(values), maximum)]
	bounded := make([]string, len(values))
	for index, value := range values {
		bounded[index] = boundedTextBytes(value, maximumBytes)
	}

	return bounded
}

func boundedURLs(values []string, maximum int) []string {
	bounded := make([]string, 0, min(len(values), maximum))
	for _, value := range values {
		if len(value) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		bounded = append(bounded, value)
		if len(bounded) == maximum {
			break
		}
	}

	return bounded
}

func imageMetadataFromPage(in []pageparse.ImageMetadata) []yagocrawlcontract.ImageMetadata {
	out := make(
		[]yagocrawlcontract.ImageMetadata,
		0,
		min(len(in), yagocrawlcontract.MaximumDocumentImages),
	)
	for _, image := range in {
		if len(image.URL) > yagocrawlcontract.MaximumCrawlURLBytes {
			continue
		}
		out = append(out, yagocrawlcontract.ImageMetadata{
			URL: image.URL,
			AltText: boundedTextBytes(
				image.AltText,
				yagocrawlcontract.MaximumDocumentMetadataBytes,
			),
		})
		if len(out) == yagocrawlcontract.MaximumDocumentImages {
			break
		}
	}

	return out
}

func documentCanonicalURL(page pageparse.ParsedPage) string {
	if page.CanonicalURL != "" &&
		len(page.CanonicalURL) <= yagocrawlcontract.MaximumCrawlURLBytes {
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
		values["description"] = boundedTextBytes(
			page.Description,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
	}
	if page.Author != "" {
		values["author"] = boundedTextBytes(
			page.Author,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
	}
	if page.Keywords != "" {
		values["keywords"] = boundedTextBytes(
			page.Keywords,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
	}
	if page.Publisher != "" {
		values["publisher"] = boundedTextBytes(
			page.Publisher,
			yagocrawlcontract.MaximumDocumentMetadataBytes,
		)
	}
	return values
}
