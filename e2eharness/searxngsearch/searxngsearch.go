//go:build e2e

// Package searxngsearch queries a running SearXNG instance's JSON search API.
package searxngsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

type Result struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type response struct {
	Results []Result `json:"results"`
}

func Search(t *testing.T, ctx context.Context, baseURL, query string) []Result {
	t.Helper()
	requestURL := baseURL + "/search?" + url.Values{
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

	var decoded response
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	return decoded.Results
}

func SearchOneResult(t *testing.T, ctx context.Context, baseURL, query string) Result {
	t.Helper()
	results := Search(t, ctx, baseURL, query)
	if len(results) == 0 {
		t.Fatal("search response carries no results")
	}
	return results[0]
}
