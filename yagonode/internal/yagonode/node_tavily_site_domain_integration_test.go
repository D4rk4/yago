package yagonode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

func TestTavilySingleIncludeDomainFindsLocalSubdomain(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	document := documentstore.Document{
		NormalizedURL: "https://docs.parent.example/guide",
		Title:         "Needle guide",
		ExtractedText: "Needle guide",
		Language:      "en",
	}
	if err := index.Index(t.Context(), document); err != nil {
		t.Fatalf("Index: %v", err)
	}

	const key = "site-domain-integration-key"
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		tavilyapi.PathSearch,
		strings.NewReader(`{"query":"needle","include_domains":["parent.example"]}`),
	)
	request.Header.Set("Authorization", "Bearer "+key)
	tavilyapi.NewSearchEndpointWithAccess(
		searchlocal.NewSearcher(index),
		nil,
		tavilyapi.SearchAccessPolicy{BearerToken: key},
	).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response tavilyapi.SearchResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != document.NormalizedURL {
		t.Fatalf("results = %#v", response.Results)
	}
}
