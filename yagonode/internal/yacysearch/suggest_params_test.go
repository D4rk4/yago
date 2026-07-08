package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagoproto"
)

func TestSuggestCountClampsToUpstreamBounds(t *testing.T) {
	cases := map[string]int{
		"":    publicSuggestionLimit,
		"x":   publicSuggestionLimit,
		"0":   publicSuggestionLimit,
		"-5":  publicSuggestionLimit,
		"5":   5,
		"30":  suggestMaxCount,
		"100": suggestMaxCount,
	}
	for raw, want := range cases {
		if got := suggestCount(raw); got != want {
			t.Errorf("suggestCount(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestSuggestTimeoutClampsToUpstreamBounds(t *testing.T) {
	cases := map[string]time.Duration{
		"":         suggestDefaultTimeout,
		"x":        suggestDefaultTimeout,
		"0":        suggestDefaultTimeout,
		"-1":       suggestDefaultTimeout,
		"500":      500 * time.Millisecond,
		"99999999": suggestMaxTimeout,
	}
	for raw, want := range cases {
		if got := suggestTimeout(raw); got != want {
			t.Errorf("suggestTimeout(%q) = %s, want %s", raw, got, want)
		}
	}
}

func TestSanitizeCallbackAcceptsOnlyBareIdentifiers(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"jsonpCallback":         "jsonpCallback",
		"_hidden":               "_hidden",
		"$jq":                   "$jq",
		"cb_12$":                "cb_12$",
		"1leading":              "",
		"has space":             "",
		"has-dash":              "",
		"props.path":            "",
		strings.Repeat("a", 65): "",
	}
	for raw, want := range cases {
		if got := sanitizeCallback(raw); got != want {
			t.Errorf("sanitizeCallback(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseSuggestParamsFallsBackToQAndParsesEveryField(t *testing.T) {
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/suggest.json?q=linux&count=3&timeout=250&callback=cb",
		nil,
	)
	params := parseSuggestParams(req)

	if params.query != "linux" {
		t.Errorf("query = %q, want linux (q fallback)", params.query)
	}
	if params.limit != 3 {
		t.Errorf("limit = %d, want 3", params.limit)
	}
	if params.timeout != 250*time.Millisecond {
		t.Errorf("timeout = %s, want 250ms", params.timeout)
	}
	if params.callback != "cb" {
		t.Errorf("callback = %q, want cb", params.callback)
	}
}

func TestSuggestJSONEndpointWrapsJSONPAndSetsCORS(t *testing.T) {
	endpoint := suggestEndpoint{
		index:       indexSuggester{search: titledSearch("Linux kernel")},
		suggestions: newRecentQueries(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, yagoproto.PathSuggestJSON+"?q=linux&callback=cb", nil,
	)
	endpoint.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS header = %q, want *", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("content type = %q, want javascript", got)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "cb(") || !strings.HasSuffix(body, ");") {
		t.Fatalf("JSONP body %q is not wrapped in the callback", body)
	}
	if !strings.Contains(body, "Linux kernel") {
		t.Fatalf("JSONP body %q missing the suggestion", body)
	}
}

func TestSuggestJSONEndpointSetsCORSWithoutCallback(t *testing.T) {
	endpoint := suggestEndpoint{
		index:       indexSuggester{search: titledSearch("Debian")},
		suggestions: newRecentQueries(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, yagoproto.PathSuggestJSON+"?q=deb", nil,
	)
	endpoint.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS header = %q, want *", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-suggestions+json" {
		t.Fatalf("content type = %q, want suggestions json", got)
	}
}

func TestSuggestJSONEndpointHonoursCount(t *testing.T) {
	endpoint := suggestEndpoint{
		index:       indexSuggester{search: titledSearch("Linux one", "Linux two")},
		suggestions: newRecentQueries(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, yagoproto.PathSuggestJSON+"?q=linux&count=1", nil,
	)
	endpoint.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Linux one") || strings.Contains(body, "Linux two") {
		t.Fatalf("count=1 body %q must keep only the first suggestion", body)
	}
}

func TestSuggestXMLEndpointSetsCORS(t *testing.T) {
	endpoint := suggestXMLEndpoint{
		index:       indexSuggester{search: titledSearch("Fedora")},
		suggestions: newRecentQueries(),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, yagoproto.PathSuggestXML+"?q=fed", nil,
	)
	endpoint.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS header = %q, want *", got)
	}
}
