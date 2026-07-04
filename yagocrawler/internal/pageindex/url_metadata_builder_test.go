package pageindex_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

func TestBuildMetadataRoundTrips(t *testing.T) {
	page := pageparse.ParsedPage{
		URL:      "http://example.com/path?q=a,b={c}&d=e",
		Title:    "Title, with {special}=chars",
		Language: "en",
		Text:     "some body text",
		Links:    []string{"http://example.com/a", "http://other.com/b"},
	}
	loadedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	row := pageindex.BuildMetadata(page, pageparse.BuildPageStats(page), loadedAt)

	parsed, err := yagomodel.ParseURIMetadataRow(row.String())
	if err != nil {
		t.Fatalf("ParseURIMetadataRow(%q): %v", row.String(), err)
	}

	wantHash, err := yagomodel.HashURL(page.URL)
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	gotHash, err := parsed.URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	if gotHash != wantHash {
		t.Errorf("url hash = %q, want %q", gotHash, wantHash)
	}

	decodedURL, err := yagomodel.DecodeWireForm(t.Context(), parsed.Properties["url"])
	if err != nil {
		t.Fatalf("decode url value: %v", err)
	}
	if decodedURL != page.URL {
		t.Errorf("decoded url = %q, want %q", decodedURL, page.URL)
	}
}
