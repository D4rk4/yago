package yagonode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type indexStatsScript struct {
	stats searchindex.IndexStats
	err   error
	calls int
}

func (s *indexStatsScript) Index(context.Context, documentstore.Document) error { return nil }

func (s *indexStatsScript) Delete(context.Context, string) error { return nil }

func (s *indexStatsScript) Search(
	context.Context,
	searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	return searchindex.SearchResultSet{}, nil
}

func (s *indexStatsScript) Stats(context.Context) (searchindex.IndexStats, error) {
	s.calls++
	if s.err != nil {
		return searchindex.IndexStats{}, s.err
	}

	return s.stats, nil
}

func TestIndexStatsEndpointReturnsSearchIndexStats(t *testing.T) {
	index := &indexStatsScript{
		stats: searchindex.IndexStats{
			Documents: 7,
			Backend:   "bleve-memory",
			UpdatedAt: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		},
	}
	endpoint := indexStatsEndpoint{
		index: index,
		now: func() time.Time {
			return time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathIndexStats, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"generatedAt"`)) ||
		bytes.Contains(rec.Body.Bytes(), []byte(`"GeneratedAt"`)) {
		t.Fatalf("response uses unstable JSON field names: %s", rec.Body.String())
	}

	var got indexStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Available ||
		got.GeneratedAt != "2026-07-02T11:00:00Z" ||
		got.Backend != "bleve-memory" ||
		got.Documents != 7 ||
		got.UpdatedAt != "2026-07-02T10:00:00Z" ||
		index.calls != 1 {
		t.Fatalf("response = %#v calls=%d", got, index.calls)
	}
}

func TestIndexStatsEndpointReportsUnavailableIndex(t *testing.T) {
	endpoint := indexStatsEndpoint{
		now: func() time.Time {
			return time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathIndexStats, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}

	var got indexStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Available || got.GeneratedAt != "2026-07-02T11:00:00Z" {
		t.Fatalf("response = %#v", got)
	}
}

func TestIndexStatsEndpointReportsStatsErrors(t *testing.T) {
	endpoint := newIndexStatsEndpoint(&indexStatsScript{err: errors.New("stats failed")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathIndexStats, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("search index stats failed")) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestIndexStatsEndpointRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathIndexStats, nil)
	newIndexStatsEndpoint(&indexStatsScript{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
	}
}

func TestFormattedIndexStatsTimeOmitsZero(t *testing.T) {
	if got := formattedIndexStatsTime(time.Time{}); got != "" {
		t.Fatalf("zero time = %q, want empty", got)
	}
}
