//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/pollwait"
)

func waitForManticoreIndexedHit(
	t *testing.T,
	ctx context.Context,
	manticoreURL, expectedURL string,
) searchHit {
	t.Helper()
	var found searchHit
	ok := pollwait.For(30*time.Second, func() bool {
		hit, ok := manticoreSearchOnce(t, ctx, manticoreURL, expectedURL)
		if !ok {
			return false
		}
		found = hit
		return true
	})
	if !ok {
		t.Fatal("manticore never indexed the crawled page")
	}
	return found
}

func manticoreSearchOnce(
	t *testing.T,
	ctx context.Context,
	manticoreURL, expectedURL string,
) (searchHit, bool) {
	t.Helper()
	query, err := json.Marshal(map[string]any{
		"table": manticoreTable,
		"query": map[string]any{"match": map[string]any{"url": expectedURL}},
	})
	if err != nil {
		t.Fatalf("marshal manticore query: %v", err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, manticoreURL+"/search", bytes.NewReader(query),
	)
	if err != nil {
		t.Fatalf("build manticore search request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
