package publicportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type emptySource struct{}

func (emptySource) Search(context.Context, string, int, int) (SearchResults, error) {
	return SearchResults{Query: "void"}, nil
}

func TestPortalAccessibilityLandmarks(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=go", nil)
	New(cachedSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		`<a class="skip" href="#serp">Skip to results</a>`,
		`id="serp" tabindex="-1"`,
		`<ul class="results">`, `<li class="result">`,
		`<p class="meta" role="status">`,
		"focus-visible",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portal missing a11y landmark %q", want)
		}
	}
}

func TestPortalAnnouncesEmptyResults(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=void", nil)
	New(emptySource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `<p class="meta" role="status">Nothing found.</p>`) {
		t.Fatalf("empty state not announced: %s", body)
	}
	if strings.Contains(body, `<ul class="results">`) {
		t.Fatal("empty result set must not render a list")
	}
}
