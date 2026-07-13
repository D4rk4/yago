package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestHTMLEndpointHidesInternalWebFailureSource(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceWeb,
			Reason: "web-search fallback provider failed",
		}},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.test/yacysearch.html?query=drunklab",
		nil,
	)
	htmlEndpoint{search: search, suggestions: newRecentQueries()}.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, "web: web-search fallback provider failed") ||
		strings.Contains(body, "ddgs:") {
		t.Fatalf("failure banner = %s", body)
	}
}
