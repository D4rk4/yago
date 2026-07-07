//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

type searchResult struct {
	URL string `json:"url"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

func searchOneResult(
	t *testing.T,
	ctx context.Context,
	searxngBaseURL string,
	query string,
) searchResult {
	t.Helper()
	requestURL := searxngBaseURL + "/search?" + url.Values{
		"q":      {query},
		"format": {"json"},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("build search request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search request: status %d", resp.StatusCode)
	}

	var decoded searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if len(decoded.Results) == 0 {
		t.Fatal("search response carries no results")
	}
	return decoded.Results[0]
}
