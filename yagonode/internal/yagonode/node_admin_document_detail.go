package yagonode

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	documentDetailContentBytes = 32 << 10
	documentDetailValueBytes   = 8 << 10
	documentDetailItems        = 50
)

type documentDetailSource struct {
	documents documentstore.DocumentDirectory
}

func newDocumentDetailSource(documents documentstore.DocumentDirectory) documentDetailSource {
	return documentDetailSource{documents: documents}
}

func documentDetailAdmin(
	stored documentstore.StoredDocuments,
) adminui.DocumentDetailSource {
	documents, _ := stored.(documentstore.DocumentDirectory)
	if documents == nil {
		return nil
	}

	return newDocumentDetailSource(documents)
}

func (s documentDetailSource) DocumentDetail(
	ctx context.Context,
	key string,
) (adminui.DocumentDetail, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" || s.documents == nil {
		return adminui.DocumentDetail{}, false, nil
	}
	document, found, err := s.documents.Document(ctx, key)
	if err != nil {
		return adminui.DocumentDetail{}, found, fmt.Errorf("read stored document: %w", err)
	}
	if !found {
		return adminui.DocumentDetail{}, false, nil
	}

	return documentDetail(document), true, nil
}

func documentDetail(document documentstore.Document) adminui.DocumentDetail {
	preview := boundedDocumentDetailText(document.ExtractedText, documentDetailContentBytes)
	return adminui.DocumentDetail{
		Extraction: adminui.DocumentExtractionDetail{
			Generation: document.ExtractionGeneration,
			Current:    yagocrawlcontract.CurrentExtractionGeneration,
		},
		Key: boundedDocumentDetailValue(document.NormalizedURL),
		URL: boundedDocumentDetailValue(documentURL(document)),
		NormalizedURL: boundedDocumentDetailText(
			document.NormalizedURL,
			documentDetailValueBytes,
		),
		CanonicalURL: boundedDocumentDetailText(
			document.CanonicalURL,
			documentDetailValueBytes,
		),
		RepresentativeURL: boundedDocumentDetailText(
			document.RepresentativeURL,
			documentDetailValueBytes,
		),
		Title: boundedDocumentDetailText(
			document.Title,
			documentDetailValueBytes,
		),
		Headings:       boundedDocumentDetailStrings(document.Headings),
		ContentPreview: preview,
		ContentBytes:   len(document.ExtractedText),
		ContentPreviewTruncated: documentDetailPreviewTruncated(
			document.ExtractedText,
			preview,
		),
		RawContentReference: boundedDocumentDetailText(
			document.RawContentReference,
			documentDetailValueBytes,
		),
		Language: boundedDocumentDetailText(
			document.Language,
			documentDetailValueBytes,
		),
		ContentType: boundedDocumentDetailText(
			document.ContentType,
			documentDetailValueBytes,
		),
		FetchStatus: boundedDocumentDetailText(
			document.FetchStatus,
			documentDetailValueBytes,
		),
		FetchedAt:        formatDocumentTime(document.FetchedAt),
		IndexedAt:        formatDocumentTime(document.IndexedAt),
		PublishedAt:      formatDocumentTime(document.PublishedAt),
		ModifiedAt:       formatDocumentTime(document.ModifiedAt),
		FirstSeenAt:      formatDocumentTime(document.FirstSeenAt),
		ContentChangedAt: formatDocumentTime(document.ContentChangedAt),
		DateConfidence:   document.DateConfidence,
		DateSource: boundedDocumentDetailText(
			document.DateSource,
			documentDetailValueBytes,
		),
		ContentHash: boundedDocumentDetailText(
			document.ContentHash,
			documentDetailValueBytes,
		),
		ClusterID: boundedDocumentDetailText(
			document.ClusterID,
			documentDetailValueBytes,
		),
		Quality:              documentQualityDetail(document.ContentQuality),
		Safety:               documentSafetyDetail(document.ContentSafety),
		Metadata:             boundedDocumentDetailMetadata(document.Metadata),
		HeadingsTotal:        len(document.Headings),
		MetadataTotal:        len(document.Metadata),
		Outlinks:             boundedDocumentDetailStrings(document.Outlinks),
		OutlinksTotal:        len(document.Outlinks),
		Inlinks:              boundedDocumentDetailInlinks(document.Inlinks),
		InlinksTotal:         len(document.Inlinks),
		OutboundAnchors:      boundedDocumentDetailOutboundAnchors(document.OutboundAnchors),
		OutboundAnchorsTotal: len(document.OutboundAnchors),
		Images:               boundedDocumentDetailImages(document.Images),
		ImagesTotal:          len(document.Images),
	}
}

func documentDetailPreviewTruncated(content, preview string) bool {
	return len(strings.ToValidUTF8(content, "�")) > len(preview)
}

func boundedDocumentDetailValue(value string) string {
	return boundedDocumentDetailText(value, documentDetailValueBytes)
}

func boundedDocumentDetailText(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximum {
		return value
	}
	end := maximum
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}

	return value[:end]
}

func boundedDocumentDetailStrings(values []string) []string {
	limit := min(len(values), documentDetailItems)
	bounded := make([]string, 0, limit)
	for _, value := range values[:limit] {
		bounded = append(bounded, boundedDocumentDetailText(value, documentDetailValueBytes))
	}

	return bounded
}

func boundedDocumentDetailMetadata(values map[string]string) []adminui.DocumentMetadataDetail {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	names = names[:min(len(names), documentDetailItems)]
	metadata := make([]adminui.DocumentMetadataDetail, 0, len(names))
	for _, name := range names {
		metadata = append(metadata, adminui.DocumentMetadataDetail{
			Name:  boundedDocumentDetailText(name, documentDetailValueBytes),
			Value: boundedDocumentDetailText(values[name], documentDetailValueBytes),
		})
	}

	return metadata
}

func boundedDocumentDetailInlinks(values []documentstore.AnchorText) []adminui.DocumentLinkDetail {
	limit := min(len(values), documentDetailItems)
	links := make([]adminui.DocumentLinkDetail, 0, limit)
	for _, value := range values[:limit] {
		links = append(links, adminui.DocumentLinkDetail{
			URL:           boundedDocumentDetailText(value.URL, documentDetailValueBytes),
			Text:          boundedDocumentDetailText(value.Text, documentDetailValueBytes),
			NoFollow:      value.NoFollow,
			UserGenerated: value.UserGenerated,
			Sponsored:     value.Sponsored,
		})
	}

	return links
}

func boundedDocumentDetailOutboundAnchors(
	values []documentstore.OutboundAnchor,
) []adminui.DocumentLinkDetail {
	limit := min(len(values), documentDetailItems)
	links := make([]adminui.DocumentLinkDetail, 0, limit)
	for _, value := range values[:limit] {
		links = append(links, adminui.DocumentLinkDetail{
			URL:           boundedDocumentDetailText(value.TargetURL, documentDetailValueBytes),
			Text:          boundedDocumentDetailText(value.Text, documentDetailValueBytes),
			NoFollow:      value.NoFollow,
			UserGenerated: value.UserGenerated,
			Sponsored:     value.Sponsored,
		})
	}

	return links
}

func boundedDocumentDetailImages(
	values []documentstore.ImageMetadata,
) []adminui.DocumentImageDetail {
	limit := min(len(values), documentDetailItems)
	images := make([]adminui.DocumentImageDetail, 0, limit)
	for _, value := range values[:limit] {
		images = append(images, adminui.DocumentImageDetail{
			URL:     boundedDocumentDetailText(value.URL, documentDetailValueBytes),
			AltText: boundedDocumentDetailText(value.AltText, documentDetailValueBytes),
		})
	}

	return images
}

func documentQualityDetail(
	value documentstore.ContentQualityEvidence,
) adminui.DocumentQualityDetail {
	return adminui.DocumentQualityDetail{
		Known:                value.Known,
		Score:                value.Score,
		FunctionWordFraction: value.FunctionWordFraction,
		SymbolFraction:       value.SymbolFraction,
		AlphabeticFraction:   value.AlphabeticFraction,
		UniqueTokenFraction:  value.UniqueTokenFraction,
		SpamRisk:             value.SpamRisk,
	}
}

func documentSafetyDetail(value documentstore.ContentSafetyEvidence) adminui.DocumentSafetyDetail {
	rating := "unknown"
	switch value.Rating {
	case documentstore.SafetyGeneral:
		rating = "general"
	case documentstore.SafetyExplicit:
		rating = "explicit"
	}

	return adminui.DocumentSafetyDetail{
		Rating:              rating,
		ExplicitProbability: value.ExplicitProbability,
		Confidence:          value.Confidence,
	}
}
