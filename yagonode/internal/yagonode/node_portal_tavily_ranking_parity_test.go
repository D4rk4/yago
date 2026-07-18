package yagonode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

type portalTavilyRankingCandidates struct {
	requests []searchcore.Request
}

func (s *portalTavilyRankingCandidates) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.requests = append(s.requests, req)

	return searchcore.Response{
		Request:      req,
		TotalResults: 4,
		Availability: searchcore.ResultAvailability{Materialized: 4, Exhausted: true},
		Results: []searchcore.Result{
			{
				Title:             "Independent gaming mouse guide",
				URL:               "https://search.example/mouse-guide",
				Host:              "search.example",
				Snippet:           "Measured latency and shape comparisons for gaming mice.",
				Score:             10,
				Source:            searchcore.SourceGlobal,
				ClusterID:         "mouse-guide",
				RepresentativeURL: "https://search.example/mouse-guide",
			},
			{
				Title:             "Mirror of the independent gaming mouse guide",
				URL:               "https://mirror.example/mouse-guide",
				Host:              "mirror.example",
				Snippet:           "The same measured gaming mouse comparison.",
				Score:             9,
				Source:            searchcore.SourceRemote,
				ClusterID:         "mouse-guide",
				RepresentativeURL: "https://search.example/mouse-guide",
			},
			{
				Title:   "Gaming mouse sensor measurements",
				URL:     "https://reviews.example/sensor-measurements",
				Host:    "reviews.example",
				Snippet: "Independent sensor and click latency measurements.",
				Score:   8,
				Source:  searchcore.SourceGlobal,
			},
			{
				Title:   "Gaming mouse product specifications",
				URL:     "https://manufacturer.example/product",
				Host:    "manufacturer.example",
				Snippet: "Dimensions, mass, switches, and polling rate.",
				Score:   7,
				Source:  searchcore.SourceGlobal,
			},
		},
	}, nil
}

func TestCanonicalPortalSourceAndTavilyAdvancedRankingParity(t *testing.T) {
	t.Parallel()

	candidates := &portalTavilyRankingCandidates{}
	shared := withParsedQuery(withEffectiveWebFallbackRequest(
		searchcore.NewFinalRankingSearcher(candidates),
		webFallbackConfig{
			Provider: webFallbackProviderDDGS,
			Privacy:  webFallbackPrivacyAlways,
		},
	))
	portalResults, err := newPortalSource(shared).Search(
		t.Context(),
		"best mouse for gaming",
		"",
		0,
		10,
	)
	if err != nil {
		t.Fatalf("portal search: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		tavilyapi.PathSearch,
		strings.NewReader(`{
			"query":"best mouse for gaming",
			"search_depth":"advanced",
			"max_results":10,
			"safe_search":false
		}`),
	)
	request.Header.Set("Authorization", "Bearer parity-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	tavilyapi.NewSearchEndpointWithAccess(
		shared,
		nil,
		tavilyapi.SearchAccessPolicy{BearerToken: "parity-token"},
	).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Tavily status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var tavilyResponse tavilyapi.SearchResponse
	if err := json.NewDecoder(recorder.Body).Decode(&tavilyResponse); err != nil {
		t.Fatalf("decode Tavily response: %v", err)
	}

	if len(candidates.requests) != 2 {
		t.Fatalf("captured requests = %d, want 2", len(candidates.requests))
	}
	if !reflect.DeepEqual(candidates.requests[0], candidates.requests[1]) {
		t.Fatalf(
			"core requests differ\nportal: %+v\nTavily: %+v",
			candidates.requests[0],
			candidates.requests[1],
		)
	}
	if candidates.requests[0].Source != searchcore.SourceGlobal ||
		candidates.requests[0].Verify != searchcore.VerifyIfExist ||
		!candidates.requests[0].AllowWebFallback {
		t.Fatalf("canonical request = %+v", candidates.requests[0])
	}

	portalURLs := make([]string, 0, len(portalResults.Results))
	for _, result := range portalResults.Results {
		portalURLs = append(portalURLs, result.URL)
	}
	tavilyURLs := make([]string, 0, len(tavilyResponse.Results))
	for _, result := range tavilyResponse.Results {
		tavilyURLs = append(tavilyURLs, result.URL)
	}
	if !slices.Equal(portalURLs, tavilyURLs) {
		t.Fatalf("portal URLs = %v, Tavily URLs = %v", portalURLs, tavilyURLs)
	}
	if len(portalURLs) != 3 || slices.Contains(portalURLs, "https://mirror.example/mouse-guide") {
		t.Fatalf("deduplicated URLs = %v", portalURLs)
	}
}
