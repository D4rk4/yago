package tavilyapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestMountDoesNotRegisterUnspecifiedSearchStreamRoute(t *testing.T) {
	mux := http.NewServeMux()
	Mount(
		mux,
		&fakeSearcher{},
		&fakeDocuments{rows: map[string]documentstore.Document{}},
		SearchAccessPolicy{BearerToken: searchTestKey},
		nil,
	)

	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/search/stream", nil,
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("POST /search/stream status = %d, want 404", response.Code)
	}
}
