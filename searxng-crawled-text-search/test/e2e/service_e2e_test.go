//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/dockernetwork"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/searxngsearch"
	"github.com/nikitakarpei/yacy-rwi-node/searchdocument"
)

const (
	seededTitle   = "Riverside Wildflower Guide"
	seededURL     = "https://example.invalid/wildflower-guide"
	seededContent = "A field guide to wildflowers found along riverside trails."
)

func TestCrawledTextSearchReturnsSeededDocument(t *testing.T) {
	ctx := context.Background()

	network := dockernetwork.New(t, ctx)

	elasticsearchHostURL := startElasticsearch(t, ctx, network.Name)
	seedDocument(t, ctx, elasticsearchHostURL, "wildflower-guide", searchdocument.Document{
		Title:     seededTitle,
		URL:       seededURL,
		Content:   seededContent,
		CrawledAt: time.Now().UTC(),
		Language:  "en",
	})

	searxngBaseURL := startSearXNG(t, ctx, network.Name, elasticsearchNetworkURL())

	result := searxngsearch.SearchOneResult(t, ctx, searxngBaseURL, "!"+engineBang+" wildflower")

	if result.Title != seededTitle {
		t.Errorf("result title = %q, want %q", result.Title, seededTitle)
	}
	if result.URL != seededURL {
		t.Errorf("result url = %q, want %q", result.URL, seededURL)
	}
	if !strings.Contains(result.Content, "wildflower") {
		t.Errorf("result content = %q, want it to contain %q", result.Content, "wildflower")
	}

	noResults := searxngsearch.Search(t, ctx, searxngBaseURL, "!"+engineBang+" nonexistentterm")
	if len(noResults) != 0 {
		t.Errorf("no-match search returned %d results, want 0", len(noResults))
	}
}
