package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLSampledSearchShowsLowerBoundWithoutFailureWarning(t *testing.T) {
	t.Parallel()

	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Sample result", URL: "https://example.org/sample",
		}},
		Coverage: &searchcore.SearchCoverage{
			CompletedShards: 8, SampledShards: 8, TotalShards: 8, Complete: false,
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet,
		"http://node.test/yacysearch.html?query=sample&resource=local", nil,
	)

	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if recorder.Code != http.StatusOK ||
		!strings.Contains(body, "At least 1 result") ||
		!strings.Contains(body, ">Sample result</a>") {
		t.Fatalf("status=%d body=%s", recorder.Code, body)
	}
	if strings.Contains(body, "class=\"partial-failure\"") ||
		strings.Contains(body, "local search deadline exceeded") {
		t.Fatalf("usable sampled results were shown as a search failure: %s", body)
	}
}

func TestHTMLDistinguishesPartialAndCompleteEmptySearches(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		coverage   *searchcore.SearchCoverage
		wantStatus string
		wantEmpty  bool
	}{
		{
			name: "partial empty window",
			coverage: &searchcore.SearchCoverage{
				CompletedShards: 2, TotalShards: 8, Complete: false,
			},
			wantStatus: "Partial search: no results found so far",
		},
		{
			name:       "complete honest empty result",
			coverage:   &searchcore.SearchCoverage{CompletedShards: 8, TotalShards: 8, Complete: true},
			wantStatus: "0 results",
			wantEmpty:  true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			search := &fakeSearch{response: searchcore.Response{Coverage: test.coverage}}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet,
				"http://node.test/yacysearch.html?query=unmatched&resource=local", nil,
			)
			htmlEndpoint{
				search:      search,
				suggestions: newRecentQueries(),
			}.ServeHTTP(
				recorder,
				request,
			)
			body := recorder.Body.String()
			if recorder.Code != http.StatusOK || !strings.Contains(body, test.wantStatus) {
				t.Fatalf("status=%d body=%s", recorder.Code, body)
			}
			if strings.Contains(body, "class=\"partial-failure\"") {
				t.Fatalf("empty answer was incorrectly rendered as a failure: %s", body)
			}
			if strings.Contains(body, `id="nothing-found"`) != test.wantEmpty {
				t.Fatalf("empty-result notice element presence = %v, want %v: %s",
					strings.Contains(body, `id="nothing-found"`), test.wantEmpty, body)
			}
		})
	}
}
