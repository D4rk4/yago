package publicportal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfiguredBaseURLOverridesRequestOrigin(t *testing.T) {
	SetBaseURLProvider(func() string { return "https://search.example.org" })
	t.Cleanup(func() { SetBaseURLProvider(func() string { return "" }) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, osddPath, nil)
	NewOpenSearch().Describe(rec, req)
	if !strings.Contains(rec.Body.String(), "https://search.example.org/?q={searchTerms}") {
		t.Fatalf("configured origin missing: %s", rec.Body.String())
	}

	SetBaseURLProvider(func() string { return "" })
	rec = httptest.NewRecorder()
	NewOpenSearch().Describe(rec, req)
	if !strings.Contains(rec.Body.String(), "http://"+req.Host+"/?q={searchTerms}") {
		t.Fatalf("request-derived origin missing: %s", rec.Body.String())
	}

	// A nil provider is ignored, and an unset provider reads empty.
	SetBaseURLProvider(nil)
	baseURLProvider.Store(nil)
	if got := configuredBaseURL(); got != "" {
		t.Fatalf("unset provider = %q", got)
	}
	SetBaseURLProvider(func() string { return "" })
}
