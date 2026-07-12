package searchindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	storedCandidateField                      = "_candidate"
	maximumStoredCandidateTitleBytes          = 1024
	maximumStoredCandidateSnippetBytes        = 2048
	maximumStoredCandidateClusterBytes        = 256
	maximumStoredCandidateRepresentativeBytes = 8192
	maximumStoredCandidateAuthorBytes         = 2048
	maximumStoredCandidateKeywordsBytes       = 2048
	maximumStoredCandidatePublisherBytes      = 1024
	maximumStoredCandidateLanguageBytes       = 64
	maximumStoredCandidateContentTypeBytes    = 256
	maximumStoredCandidateImages              = 5
	maximumStoredCandidateImageURLBytes       = 2048
	maximumStoredCandidateImageAltBytes       = 512
)

type storedCandidateProjection struct {
	Title                  string                               `json:"t,omitempty"`
	Snippet                string                               `json:"s,omitempty"`
	ClusterID              string                               `json:"c,omitempty"`
	RepresentativeURL      string                               `json:"r,omitempty"`
	RepresentativeComplete bool                                 `json:"rc"`
	ContentQuality         documentstore.ContentQualityEvidence `json:"q"`
	ContentSafety          documentstore.ContentSafetyEvidence  `json:"x"`
	PublishedAt            time.Time                            `json:"d,omitempty"`
	DateConfidence         float64                              `json:"dc,omitempty"`
	Author                 string                               `json:"a,omitempty"`
	AuthorComplete         bool                                 `json:"ac"`
	Keywords               string                               `json:"k,omitempty"`
	Publisher              string                               `json:"p,omitempty"`
	Language               string                               `json:"l,omitempty"`
	LanguageComplete       bool                                 `json:"lc"`
	ContentType            string                               `json:"m,omitempty"`
	ContentTypeComplete    bool                                 `json:"mc"`
	Size                   int                                  `json:"z,omitempty"`
	HasImages              bool                                 `json:"ih,omitempty"`
	Images                 []documentstore.ImageMetadata        `json:"i,omitempty"`
}

type searchHitProjection struct {
	document  documentstore.Document
	size      int
	candidate bool
}

func encodeStoredCandidateProjection(doc documentstore.Document) (string, error) {
	projection := newStoredCandidateProjection(doc)
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("marshal stored candidate: %w", err)
	}

	return string(encoded), nil
}

func newStoredCandidateProjection(doc documentstore.Document) storedCandidateProjection {
	publishedAt, dateConfidence := documentstore.PublicationDate(doc)
	title, _ := boundedStoredCandidateString(doc.Title, maximumStoredCandidateTitleBytes)
	lead, _ := boundedStoredCandidateString(
		snippet(doc.ExtractedText, documentTitle(doc)),
		maximumStoredCandidateSnippetBytes,
	)
	representative, representativeComplete := boundedStoredCandidateString(
		doc.RepresentativeURL,
		maximumStoredCandidateRepresentativeBytes,
	)
	author, authorComplete := boundedStoredCandidateString(
		doc.Metadata["author"],
		maximumStoredCandidateAuthorBytes,
	)
	keywords, _ := boundedStoredCandidateString(
		doc.Metadata["keywords"],
		maximumStoredCandidateKeywordsBytes,
	)
	publisher, _ := boundedStoredCandidateString(
		doc.Metadata["publisher"],
		maximumStoredCandidatePublisherBytes,
	)
	language, languageComplete := boundedStoredCandidateString(
		doc.Language,
		maximumStoredCandidateLanguageBytes,
	)
	contentType, contentTypeComplete := boundedStoredCandidateString(
		doc.ContentType,
		maximumStoredCandidateContentTypeBytes,
	)

	return storedCandidateProjection{
		Title:                  title,
		Snippet:                lead,
		ClusterID:              boundedStoredCandidateClusterID(doc.ClusterID),
		RepresentativeURL:      representative,
		RepresentativeComplete: representativeComplete,
		ContentQuality:         doc.ContentQuality,
		ContentSafety:          doc.ContentSafety,
		PublishedAt:            publishedAt,
		DateConfidence:         dateConfidence,
		Author:                 author,
		AuthorComplete:         authorComplete,
		Keywords:               keywords,
		Publisher:              publisher,
		Language:               language,
		LanguageComplete:       languageComplete,
		ContentType:            contentType,
		ContentTypeComplete:    contentTypeComplete,
		Size:                   len(doc.ExtractedText),
		HasImages:              len(doc.Images) > 0,
		Images:                 boundedStoredCandidateImages(doc.Images),
	}
}

func boundedStoredCandidateString(value string, maximumBytes int) (string, bool) {
	if len(value) <= maximumBytes {
		return value, true
	}
	end := maximumBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}

	return value[:end], false
}

func boundedStoredCandidateClusterID(clusterID string) string {
	if len(clusterID) <= maximumStoredCandidateClusterBytes {
		return clusterID
	}
	identity := sha256.Sum256([]byte(clusterID))

	return hex.EncodeToString(identity[:])
}

func boundedStoredCandidateImages(
	images []documentstore.ImageMetadata,
) []documentstore.ImageMetadata {
	limit := min(len(images), maximumStoredCandidateImages)
	bounded := make([]documentstore.ImageMetadata, 0, limit)
	for _, image := range images[:limit] {
		url, _ := boundedStoredCandidateString(
			image.URL,
			maximumStoredCandidateImageURLBytes,
		)
		alt, _ := boundedStoredCandidateString(
			image.AltText,
			maximumStoredCandidateImageAltBytes,
		)
		bounded = append(bounded, documentstore.ImageMetadata{URL: url, AltText: alt})
	}

	return bounded
}

func decodeStoredCandidateProjection(
	hit *search.DocumentMatch,
) (storedCandidateProjection, error) {
	encoded, ok := hit.Fields[storedCandidateField].(string)
	if !ok || encoded == "" {
		return storedCandidateProjection{}, fmt.Errorf("stored candidate unavailable")
	}
	var projection storedCandidateProjection
	if err := json.Unmarshal([]byte(encoded), &projection); err != nil {
		return storedCandidateProjection{}, fmt.Errorf("unmarshal stored candidate: %w", err)
	}

	return projection, nil
}

func (p storedCandidateProjection) supports(req SearchRequest) bool {
	if req.Near || !p.RepresentativeComplete {
		return false
	}
	if req.Author != "" && !p.AuthorComplete {
		return false
	}
	if req.Language != "" && !p.LanguageComplete {
		return false
	}
	contentDomain := strings.ToLower(req.ContentDomain)
	usesContentType := req.FileType != "" || contentDomain == "audio" ||
		contentDomain == "video" || contentDomain == "app"

	return !usesContentType || p.ContentTypeComplete
}

func (p storedCandidateProjection) document(hitID string) documentstore.Document {
	images := append([]documentstore.ImageMetadata(nil), p.Images...)
	if p.HasImages && len(images) == 0 {
		images = []documentstore.ImageMetadata{{}}
	}

	return documentstore.Document{
		NormalizedURL:     hitID,
		Title:             p.Title,
		ExtractedText:     p.Snippet,
		ContentQuality:    p.ContentQuality,
		ContentSafety:     p.ContentSafety,
		Language:          p.Language,
		ContentType:       p.ContentType,
		PublishedAt:       p.PublishedAt,
		DateConfidence:    p.DateConfidence,
		ClusterID:         p.ClusterID,
		RepresentativeURL: p.RepresentativeURL,
		Images:            images,
		Metadata: map[string]string{
			"author":    p.Author,
			"keywords":  p.Keywords,
			"publisher": p.Publisher,
		},
	}
}

func (p searchHitProjection) result(
	ctx context.Context,
	hit *search.DocumentMatch,
	req SearchRequest,
) (SearchResult, error) {
	if !p.candidate {
		return searchResultFromStoredDocument(ctx, hit, p.document, req)
	}
	result := searchResultFromDocument(hit, p.document, req)
	result.Size = p.size

	return result, nil
}

func (b *BleveDiskIndex) loadSearchHitProjection(
	ctx context.Context,
	hit *search.DocumentMatch,
	req SearchRequest,
) (searchHitProjection, bool, error) {
	if req.CandidateOnly && b.storedCandidates {
		candidate, err := decodeStoredCandidateProjection(hit)
		if err == nil && candidate.supports(req) && b.documentPresence != nil {
			found, presenceErr := b.documentPresence.DocumentExists(ctx, hit.ID)
			if presenceErr != nil {
				return searchHitProjection{}, false, fmt.Errorf(
					"check stored candidate presence: %w",
					presenceErr,
				)
			}
			if !found {
				return searchHitProjection{}, false, nil
			}
			return searchHitProjection{
				document:  candidate.document(hit.ID),
				size:      candidate.Size,
				candidate: true,
			}, true, nil
		}
	}
	doc, found, err := b.documents.Document(ctx, hit.ID)
	if err != nil {
		return searchHitProjection{}, false, fmt.Errorf(
			"load stored search document: %w",
			err,
		)
	}
	if !found {
		return searchHitProjection{}, false, nil
	}

	return searchHitProjection{document: doc}, true, nil
}

func storedSearchFields(req SearchRequest, storedCandidates bool) []string {
	fields := []string{documentAnalyzerField}
	if req.CandidateOnly && storedCandidates {
		fields = append(fields, storedCandidateField)
	}

	return fields
}
