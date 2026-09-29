package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/websearch"
)

type portalTavilyEmptyPrimary struct {
	requests []searchcore.Request
}

func (s *portalTavilyEmptyPrimary) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.requests = append(s.requests, req)

	return searchcore.Response{Request: req}, nil
}

type portalTavilyWebResults struct {
	results []websearch.Result
}

func (p portalTavilyWebResults) Search(
	context.Context,
	string,
	int,
) ([]websearch.Result, error) {
	return slices.Clone(p.results), nil
}

func TestPortalAndTavilyShareWebRelevanceAcrossSearchDepths(t *testing.T) {
	query := "alpha beta"
	depths := []struct {
		name   string
		value  string
		verify searchcore.VerifyMode
	}{
		{name: "default", verify: searchcore.VerifyFalse},
		{name: "basic", value: "basic", verify: searchcore.VerifyFalse},
		{name: "fast", value: "fast", verify: searchcore.VerifyFalse},
		{name: "ultra-fast", value: "ultra-fast", verify: searchcore.VerifyFalse},
		{name: "advanced", value: "advanced", verify: searchcore.VerifyIfExist},
	}
	for _, depth := range depths {
		t.Run(depth.name, func(t *testing.T) {
			assertPortalTavilyWebRelevance(t, query, depth.value, depth.verify)
		})
	}
}

func assertPortalTavilyWebRelevance(
	t *testing.T,
	query, depth string,
	verify searchcore.VerifyMode,
) {
	t.Helper()
	primary := &portalTavilyEmptyPrimary{}
	provider := portalTavilyWebResults{results: []websearch.Result{
		{Title: "Unrelated site index", URL: "https://noise.example/"},
		{Title: "Alpha beta guide", URL: "https://query.example/guide"},
	}}
	fallback := websearch.NewFallbackSearcher(
		primary,
		provider,
		func(req searchcore.Request) bool { return req.AllowWebFallback },
	)
	search := withParsedQuery(withEffectiveWebFallbackRequest(
		fallback,
		webFallbackConfig{
			Enabled:  true,
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyAlways,
		},
	))
	portalResults, err := newPortalSource(search).Search(t.Context(), query, "", 0, 10)
	if err != nil {
		t.Fatalf("portal search: %v", err)
	}
	apiResponse := runTavilySearch(t, search, query, depth)
	portalURLs := make([]string, 0, len(portalResults.Results))
	for _, result := range portalResults.Results {
		portalURLs = append(portalURLs, result.URL)
	}
	apiURLs := make([]string, 0, len(apiResponse.Results))
	for _, result := range apiResponse.Results {
		apiURLs = append(apiURLs, result.URL)
	}
	wantURLs := []string{"https://query.example/guide"}
	if !slices.Equal(portalURLs, wantURLs) || !slices.Equal(apiURLs, wantURLs) {
		t.Fatalf("portal URLs = %v, Tavily URLs = %v, want %v", portalURLs, apiURLs, wantURLs)
	}
	assertPortalTavilyRequests(t, primary.requests, verify)
}

func runTavilySearch(
	t *testing.T,
	search searchcore.Searcher,
	query, depth string,
) tavilyapi.SearchResponse {
	t.Helper()
	maxResults := 10
	body, err := json.Marshal(tavilyapi.SearchRequest{
		Query: query, SearchDepth: depth, MaxResults: &maxResults,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, tavilyapi.PathSearch, strings.NewReader(string(body)),
	)
	request.Header.Set("Authorization", "Bearer synthetic-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	tavilyapi.NewSearchEndpointWithAccess(
		search,
		nil,
		tavilyapi.SearchAccessPolicy{BearerToken: "synthetic-token"},
	).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Tavily status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response tavilyapi.SearchResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode Tavily response: %v", err)
	}
	return response
}

func assertPortalTavilyRequests(
	t *testing.T,
	requests []searchcore.Request,
	verify searchcore.VerifyMode,
) {
	t.Helper()
	if len(requests) != 2 {
		t.Fatalf("primary requests = %d, want portal and Tavily", len(requests))
	}
	for _, request := range requests {
		if request.Source != searchcore.SourceGlobal || !request.AllowWebFallback {
			t.Fatalf("source or web opt-in changed: %+v", request)
		}
	}
	if requests[0].Verify != searchcore.VerifyIfExist || requests[1].Verify != verify {
		t.Fatalf(
			"portal/Tavily verify modes = %q/%q, want %q/%q",
			requests[0].Verify,
			requests[1].Verify,
			searchcore.VerifyIfExist,
			verify,
		)
	}
}
