package pageindex_test

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBuildMetadataRoundTrips(t *testing.T) {
	page := crawlwork.ParsedPage{
		URL:      "http://example.com/path?q=a,b={c}&d=e",
		Title:    "Title, with {special}=chars",
		Language: "en",
		Text:     "some body text",
		Links:    []string{"http://example.com/a", "http://other.com/b"},
	}
	loadedAt := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	row, err := pageindex.BuildMetadata(page, pageparse.BuildPageStats(page), loadedAt)
	if err != nil {
		t.Fatalf("BuildMetadata: %v", err)
	}

	parsed, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		t.Fatalf("ParseURIMetadataRow(%q): %v", row.String(), err)
	}

	wantHash, err := yacymodel.HashURL(page.URL)
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

	decodedURL, err := yacymodel.DecodeWireForm(t.Context(), parsed.Properties["url"])
	if err != nil {
		t.Fatalf("decode url value: %v", err)
	}
	if decodedURL != page.URL {
		t.Errorf("decoded url = %q, want %q", decodedURL, page.URL)
	}
}
