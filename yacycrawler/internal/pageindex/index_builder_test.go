package pageindex_test

import (
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
)

func TestIndexBuilderBuildsPostingsAndMetadata(t *testing.T) {
	page := pageparse.ParsedPage{
		URL:         "http://example.com/path",
		Title:       "Kangaroo facts",
		Description: "A compact kangaroo page description.",
		Language:    "en",
		Text:        "kangaroo hops across the outback",
		Images: []pageparse.ImageMetadata{{
			URL:     "https://example.com/kangaroo.jpg",
			AltText: "Kangaroo",
		}},
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
	if artifacts.Document.Metadata["description"] != page.Description {
		t.Errorf("document description metadata = %q", artifacts.Document.Metadata["description"])
	}
	if len(artifacts.Document.Images) != 1 ||
		artifacts.Document.Images[0].URL != "https://example.com/kangaroo.jpg" ||
		artifacts.Document.Images[0].AltText != "Kangaroo" {
		t.Errorf("document images = %#v", artifacts.Document.Images)
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
