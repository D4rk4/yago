package publicportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recoveredSource struct{}

type missSuggestionSource struct{}

func (recoveredSource) Search(context.Context, string, string, int, int) (SearchResults, error) {
	return SearchResults{
		Query:         "golnag",
		TotalResults:  1,
		LocalCount:    1,
		Recovered:     true,
		DidYouMean:    "golang",
		DidYouMeanURL: "/?q=golang",
		Results:       []SearchResult{{Title: "Golang", URL: "https://a.example/"}},
	}, nil
}

func (missSuggestionSource) Search(
	context.Context,
	string,
	string,
	int,
	int,
) (SearchResults, error) {
	return SearchResults{
		Query:         "golnag",
		DidYouMean:    "golang",
		DidYouMeanURL: "/?q=golang",
	}, nil
}

func TestPortalRendersRecoveryLineWithSuggestion(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=golnag", nil)
	New(recoveredSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"No exact matches for “golnag” — showing close matches instead.",
		`Did you mean <a href="/?q=golang">golang</a>?`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("recovery line missing %q: %s", want, body)
		}
	}
}

func TestPortalRendersTotalMissSuggestion(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=golnag", nil)
	New(missSuggestionSource{}, false).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "No results matched. Did you mean") ||
		!strings.Contains(body, `href="/?q=golang"`) {
		t.Fatalf("total-miss suggestion missing: %s", body)
	}
}
