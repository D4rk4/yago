package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredBaseURLOverridesRequestBase(t *testing.T) {
	SetBaseURLProvider(func() string { return "https://peers.example.org" })
	t.Cleanup(func() { SetBaseURLProvider(func() string { return "" }) })

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.json?query=x",
		nil,
	)
	if got := requestBaseURL(req); got != "https://peers.example.org" {
		t.Fatalf("base = %q", got)
	}

	SetBaseURLProvider(func() string { return "" })
	if got := requestBaseURL(req); got != "http://"+req.Host {
		t.Fatalf("fallback base = %q", got)
	}
	SetBaseURLProvider(nil)
	baseURLProvider.Store(nil)
	if got := configuredBaseURL(); got != "" {
		t.Fatalf("unset provider = %q", got)
	}
	SetBaseURLProvider(func() string { return "" })
}
