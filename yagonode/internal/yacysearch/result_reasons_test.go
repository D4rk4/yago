package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLSearchRendersBoundedResultReasons(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:  "Evidence",
			URL:    "https://example.org/",
			Source: searchcore.SourceLocal,
			Evidence: searchcore.NewRankingEvidence(searchcore.RankingSignalValue{
				Signal: searchcore.SignalTitleScore,
				Value:  1,
			}),
		}},
	}}
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://node.test/yacysearch.html?query=evidence", nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(response, request)
	body := response.Body.String()
	for _, want := range []string{
		"Why this result?", "Matched the local full-text index.", "The query matched the title.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("HTML result does not contain %q: %s", want, body)
		}
	}
}
