package yacysearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestNewSuggestHandlerServesIndexSuggestions(t *testing.T) {
	search := &fakeSearch{response: searchcore.Response{
		Results: []searchcore.Result{{Title: "Golang Tutorial", URL: "https://a.example/"}},
	}}
	handler := NewSuggestHandler(search)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/admin/search/suggest?q=gol", nil,
	)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/x-suggestions+json" {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "Golang Tutorial") {
		t.Fatalf("suggestions missing index title: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/admin/search/suggest?q=gol", nil,
	)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
}
