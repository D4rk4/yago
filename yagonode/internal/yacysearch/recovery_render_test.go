package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLEndpointRendersRecoveryLine(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "Golang", URL: "https://a.example/"}},
		Recovered:    "fuzzy",
		DidYouMean:   "golang tutorial",
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "http://node.test/yacysearch.html?query=golnag", nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "No exact matches — showing close matches instead.") {
		t.Fatalf("recovery line missing: %s", body)
	}
	// html/template escapes "+" as &#43; inside attributes.
	if !strings.Contains(body, "query=golang&#43;tutorial") ||
		!strings.Contains(body, ">golang tutorial</a>?") {
		t.Fatalf("did-you-mean link missing: %s", body)
	}
}
