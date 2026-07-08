//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
)

type searchHit struct {
	Source struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"_source"`
}

type searchResponse struct {
	Hits struct {
		Hits []searchHit `json:"hits"`
	} `json:"hits"`
}

func waitForIndexedHit(
	t *testing.T,
	ctx context.Context,
	elasticsearchURL, expectedURL string,
) searchHit {
	t.Helper()
	var found searchHit
	ok := pollwait.For(30*time.Second, func() bool {
		hit, ok := searchOnce(t, ctx, elasticsearchURL, expectedURL)
		if !ok {
			return false
		}
		found = hit
		return true
	})
	if !ok {
		t.Fatal("elasticsearch never indexed the crawled page")
	}
	return found
}

func searchOnce(
	t *testing.T,
	ctx context.Context,
	elasticsearchURL, expectedURL string,
) (searchHit, bool) {
	t.Helper()
	target := elasticsearchURL + "/" + elasticsearchIndex + "/_search?q=" + "url:%22" + expectedURL + "%22"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build search request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return searchHit{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return searchHit{}, false
	}
	var body searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return searchHit{}, false
	}
	if len(body.Hits.Hits) == 0 {
		return searchHit{}, false
	}
	return body.Hits.Hits[0], true
}
