package pageindex_test

import (
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
)

func TestIndexBuilderBuildsPostingsAndMetadata(t *testing.T) {
	page := pageparse.ParsedPage{
		URL:      "http://example.com/path",
		Title:    "Kangaroo facts",
		Language: "en",
		Text:     "kangaroo hops across the outback",
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
	if artifacts.Document.ExtractedText != page.Text {
		t.Errorf("document text = %q", artifacts.Document.ExtractedText)
	}
	if artifacts.Document.ContentHash == "" {
		t.Error("expected document content hash")
	}
}
