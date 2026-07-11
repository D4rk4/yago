package pageindex_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

func TestIndexBuilderBuildsPostingsAndMetadata(t *testing.T) {
	familyFriendly := false
	page := pageparse.ParsedPage{
		URL:            "http://example.com/path",
		Title:          "Kangaroo facts",
		Description:    "A compact kangaroo page description.",
		Language:       "en",
		Text:           "kangaroo hops across the outback",
		PublishedAt:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ModifiedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		DateConfidence: 1,
		DateSource:     "json-ld",
		Images: []pageparse.ImageMetadata{{
			URL:     "https://example.com/kangaroo.jpg",
			AltText: "Kangaroo",
		}},
		OutboundAnchors: []pageparse.OutboundAnchor{{
			TargetURL: "https://example.com/facts",
			Text:      "More facts",
			NoFollow:  true,
		}},
		SafetyLabels: pageparse.SafetyLabels{
			RatingValues:   []string{"adult"},
			FamilyFriendly: &familyFriendly,
		},
	}
	artifacts, err := pageindex.NewIndexBuilder().Build(page, pageparse.BuildPageStats(page))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(artifacts.Postings) == 0 {
		t.Error("expected postings")
	}
	if len(artifacts.Metadata.Properties) == 0 {
		t.Error("expected metadata properties")
	}
	if artifacts.Document.NormalizedURL != page.URL {
		t.Errorf("document URL = %q", artifacts.Document.NormalizedURL)
	}
	if artifacts.Document.CanonicalURL != page.URL {
		t.Errorf("canonical URL = %q", artifacts.Document.CanonicalURL)
	}
	if artifacts.Document.ExtractedText != page.Text {
		t.Errorf("document text = %q", artifacts.Document.ExtractedText)
	}
	if artifacts.Document.ContentHash == "" {
		t.Error("expected document content hash")
	}
	if artifacts.Document.PublishedAt != page.PublishedAt ||
		artifacts.Document.ModifiedAt != page.ModifiedAt ||
		artifacts.Document.DateConfidence != page.DateConfidence ||
		artifacts.Document.DateSource != page.DateSource {
		t.Errorf("document dates = %#v", artifacts.Document)
	}
	if artifacts.Document.Metadata["description"] != page.Description {
		t.Errorf("document description metadata = %q", artifacts.Document.Metadata["description"])
	}
	if len(artifacts.Document.Images) != 1 ||
		artifacts.Document.Images[0].URL != "https://example.com/kangaroo.jpg" ||
		artifacts.Document.Images[0].AltText != "Kangaroo" {
		t.Errorf("document images = %#v", artifacts.Document.Images)
	}
	if len(artifacts.Document.OutboundAnchors) != 1 ||
		artifacts.Document.OutboundAnchors[0].TargetURL != "https://example.com/facts" ||
		!artifacts.Document.OutboundAnchors[0].NoFollow ||
		!artifacts.Document.OutboundAnchorEvidenceKnown {
		t.Errorf("document outbound anchors = %#v", artifacts.Document.OutboundAnchors)
	}
	if len(artifacts.Document.SafetyLabels.RatingValues) != 1 ||
		artifacts.Document.SafetyLabels.RatingValues[0] != "adult" ||
		artifacts.Document.SafetyLabels.FamilyFriendly == nil ||
		*artifacts.Document.SafetyLabels.FamilyFriendly {
		t.Errorf("document safety labels = %#v", artifacts.Document.SafetyLabels)
	}
	page.SafetyLabels.RatingValues[0] = "changed"
	familyFriendly = true
	if artifacts.Document.SafetyLabels.RatingValues[0] != "adult" ||
		*artifacts.Document.SafetyLabels.FamilyFriendly {
		t.Fatal("document safety labels retained caller mutation")
	}
}

func TestIndexBuilderPreservesCanonicalURL(t *testing.T) {
	page := pageparse.ParsedPage{
		URL:          "https://example.com/page?utm=1",
		CanonicalURL: "https://example.com/page",
		Title:        "Canonical page",
		Text:         "canonical page text",
	}
	artifacts, err := pageindex.NewIndexBuilder().Build(page, pageparse.BuildPageStats(page))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if artifacts.Document.CanonicalURL != page.CanonicalURL {
		t.Fatalf("canonical URL = %q", artifacts.Document.CanonicalURL)
	}
	if artifacts.Document.NormalizedURL != page.URL {
		t.Fatalf("normalized URL = %q", artifacts.Document.NormalizedURL)
	}
}

func TestIndexBuilderOmitsEmptyDescriptionMetadata(t *testing.T) {
	page := pageparse.ParsedPage{
		URL:   "https://example.com/page",
		Title: "Page without description",
		Text:  "page body",
	}
	artifacts, err := pageindex.NewIndexBuilder().Build(page, pageparse.BuildPageStats(page))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := artifacts.Document.Metadata["description"]; ok {
		t.Fatalf("unexpected description metadata: %v", artifacts.Document.Metadata)
	}
}
