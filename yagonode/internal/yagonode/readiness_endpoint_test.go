package yagonode

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestReadinessEndpointReturnsReadySearchIndex(t *testing.T) {
	index := &indexStatsScript{
		stats: searchindex.IndexStats{
			Documents: 11,
			Backend:   "bleve-memory",
			UpdatedAt: time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
		},
	}
	endpoint := readinessEndpoint{
		index: index,
		now: func() time.Time {
			return time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathReady, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"ready"`)) ||
		bytes.Contains(rec.Body.Bytes(), []byte(`"Ready"`)) {
		t.Fatalf("response uses unstable JSON field names: %s", rec.Body.String())
	}

	var got readinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Ready ||
		got.GeneratedAt != "2026-07-02T13:00:00Z" ||
		len(got.Checks) != 1 ||
		!got.Checks[0].Ready ||
		got.Checks[0].Name != "search_index" ||
		got.Checks[0].Backend != "bleve-memory" ||
		got.Checks[0].Documents != 11 ||
		got.Checks[0].UpdatedAt != "2026-07-02T12:00:00Z" ||
		index.calls != 1 {
		t.Fatalf("response = %#v calls=%d", got, index.calls)
	}
}

func TestReadinessEndpointReportsUnavailableSearchIndex(t *testing.T) {
	endpoint := readinessEndpoint{
		now: func() time.Time {
			return time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathReady, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}

	var got readinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Ready ||
		got.GeneratedAt != "2026-07-02T13:00:00Z" ||
		len(got.Checks) != 1 ||
		got.Checks[0].Ready ||
		got.Checks[0].Reason != "unavailable" {
		t.Fatalf("response = %#v", got)
	}
}

func TestReadinessEndpointReportsSearchIndexStatsFailure(t *testing.T) {
	endpoint := newReadinessEndpoint(&indexStatsScript{err: errors.New("stats failed")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathReady, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}

	var got readinessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Ready ||
		len(got.Checks) != 1 ||
		got.Checks[0].Ready ||
		got.Checks[0].Reason != "stats_failed" {
		t.Fatalf("response = %#v", got)
	}
}

func TestReadinessEndpointRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathReady, nil)
	newReadinessEndpoint(&indexStatsScript{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
	}
}
