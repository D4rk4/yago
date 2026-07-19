package yagonode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type documentDetailDirectory struct {
	document documentstore.Document
	found    bool
	err      error
	key      string
}

func (d *documentDetailDirectory) Document(
	_ context.Context,
	key string,
) (documentstore.Document, bool, error) {
	d.key = key

	return d.document, d.found, d.err
}

func (*documentDetailDirectory) Count(context.Context) (int, error) {
	return 1, nil
}

func TestDocumentDetailLoadsAndProjectsStoredEvidence(t *testing.T) {
	when := time.Date(2026, 7, 18, 11, 12, 13, 0, time.UTC)
	directory := &documentDetailDirectory{
		found: true,
		document: documentstore.Document{
			NormalizedURL:       "https://example.test/document",
			CanonicalURL:        "https://example.test/canonical",
			RepresentativeURL:   "https://example.test/representative",
			Title:               "Stored title",
			Headings:            []string{"First heading"},
			ExtractedText:       "stored content",
			RawContentReference: "warc:record",
			Language:            "en",
			ContentType:         "text/html",
			FetchStatus:         "200",
			FetchedAt:           when,
			IndexedAt:           when,
			ContentHash:         "content-hash",
			Metadata:            map[string]string{"description": "A description"},
			Outlinks:            []string{"https://out.example/"},
			Inlinks: []documentstore.AnchorText{{
				URL: "https://in.example/", Text: "incoming", NoFollow: true,
			}},
			OutboundAnchors: []documentstore.OutboundAnchor{{
				TargetURL: "https://target.example/", Text: "outgoing", Sponsored: true,
			}},
			Images: []documentstore.ImageMetadata{
				{URL: "https://example.test/image.png", AltText: "image"},
			},
			ContentQuality: documentstore.ContentQualityEvidence{Known: true, Score: 0.8},
			ContentSafety: documentstore.ContentSafetyEvidence{
				Rating: documentstore.SafetyGeneral, Confidence: 0.9,
			},
		},
	}
	directory.document.ExtractionGeneration = yagocrawlcontract.CurrentExtractionGeneration

	detail, found, err := newDocumentDetailSource(directory).DocumentDetail(
		t.Context(),
		" https://example.test/document ",
	)
	if err != nil || !found {
		t.Fatalf("detail found = %v, err = %v", found, err)
	}
	if directory.key != "https://example.test/document" ||
		detail.URL != "https://example.test/canonical" ||
		detail.Title != "Stored title" ||
		detail.ContentPreview != "stored content" ||
		detail.FetchedAt != "2026-07-18T11:12:13Z" ||
		len(detail.Metadata) != 1 || len(detail.Inlinks) != 1 ||
		len(detail.OutboundAnchors) != 1 || len(detail.Images) != 1 ||
		!detail.Quality.Known || detail.Safety.Rating != "general" {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Extraction.Generation != yagocrawlcontract.CurrentExtractionGeneration ||
		detail.Extraction.Current != yagocrawlcontract.CurrentExtractionGeneration {
		t.Fatalf("extraction generations = %d/%d",
			detail.Extraction.Generation, detail.Extraction.Current)
	}
}

func TestDocumentDetailBoundsContentCollectionsAndValues(t *testing.T) {
	values := make([]string, documentDetailItems+1)
	for index := range values {
		values[index] = strings.Repeat("界", documentDetailValueBytes)
	}
	document := documentstore.Document{
		NormalizedURL: "https://example.test/document",
		ExtractedText: strings.Repeat("é", documentDetailContentBytes),
		Headings:      values,
		Outlinks:      values,
		Metadata:      make(map[string]string, documentDetailItems+1),
	}
	for index := 0; index < documentDetailItems+1; index++ {
		document.Metadata[string(rune('A'+index))] = values[index]
	}

	detail := documentDetail(document)
	if len(detail.ContentPreview) > documentDetailContentBytes ||
		!utf8.ValidString(detail.ContentPreview) || !detail.ContentPreviewTruncated {
		t.Fatalf(
			"content preview bytes = %d, valid = %v, truncated = %v",
			len(
				detail.ContentPreview,
			),
			utf8.ValidString(detail.ContentPreview),
			detail.ContentPreviewTruncated,
		)
	}
	if len(detail.Headings) != documentDetailItems ||
		len(detail.Outlinks) != documentDetailItems ||
		len(detail.Metadata) != documentDetailItems ||
		detail.HeadingsTotal != documentDetailItems+1 ||
		detail.OutlinksTotal != documentDetailItems+1 ||
		detail.MetadataTotal != documentDetailItems+1 {
		t.Fatalf("bounded detail = %+v", detail)
	}
	if len(detail.Headings[0]) > documentDetailValueBytes || !utf8.ValidString(detail.Headings[0]) {
		t.Fatalf("bounded heading bytes = %d, valid = %v",
			len(detail.Headings[0]), utf8.ValidString(detail.Headings[0]))
	}
}

func TestDocumentDetailMissingAndReadFailure(t *testing.T) {
	missing := &documentDetailDirectory{}
	if _, found, err := newDocumentDetailSource(
		missing,
	).DocumentDetail(t.Context(), "x"); err != nil ||
		found {
		t.Fatalf("missing found = %v, err = %v", found, err)
	}
	want := errors.New("read failed")
	failing := &documentDetailDirectory{err: want}
	if _, _, err := newDocumentDetailSource(
		failing,
	).DocumentDetail(t.Context(), "x"); !errors.Is(
		err,
		want,
	) {
		t.Fatalf("failure = %v", err)
	}
	if _, found, err := newDocumentDetailSource(
		nil,
	).DocumentDetail(t.Context(), "x"); err != nil ||
		found {
		t.Fatalf("nil source found = %v, err = %v", found, err)
	}
}

func TestDocumentDetailAdminRequiresDirectoryAndProjectsExplicitSafety(t *testing.T) {
	t.Parallel()

	if documentDetailAdmin(nil) != nil {
		t.Fatal("nil document store exposed an Admin source")
	}
	detail := documentSafetyDetail(documentstore.ContentSafetyEvidence{
		Rating: documentstore.SafetyExplicit,
	})
	if detail.Rating != "explicit" {
		t.Fatalf("safety detail = %+v", detail)
	}
}
