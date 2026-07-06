package tavilyapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSearchEndpointDoesNotMarkWebFallbackResults(t *testing.T) {
	search := &fakeSearcher{response: searchcore.Response{
		Results: []searchcore.Result{{
			Title:   "Web Result",
			URL:     "https://web.example/page",
			Snippet: "a web snippet",
			Score:   1,
			Host:    "web.example",
			Source:  searchcore.SourceWeb,
		}},
	}}
	endpoint := searchEndpoint{
		access:    SearchAccessPolicy{BearerToken: searchTestKey},
		search:    search,
		documents: &fakeDocuments{},
		now:       fixedClock(time.Unix(100, 0), time.Unix(100, 0)),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"gap"}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSearchResponse(t, rec)
	if len(got.Results) != 1 {
		t.Fatalf("results = %#v", got.Results)
	}
	if strings.Contains(got.Results[0].Title, "[ddgs]") {
		t.Errorf("Tavily drop-in title must be unmarked, got %q", got.Results[0].Title)
	}
	if got.Results[0].Source == string(searchcore.SourceWeb) {
		t.Errorf("Tavily result must not carry the ddgs source, got %q", got.Results[0].Source)
	}
}
