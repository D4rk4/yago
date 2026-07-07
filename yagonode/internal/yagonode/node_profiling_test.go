package yagonode

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProfilingEndpointsServeOnTheOpsMux pins OPS-08: the pprof index and the
// named profiles answer on the ops mux (which the admin guard wraps), so an
// authenticated operator or a continuous-profiling scraper can pull CPU,
// heap, and goroutine profiles without a restart or a separate listener.
func TestProfilingEndpointsServeOnTheOpsMux(t *testing.T) {
	mux := http.NewServeMux()
	mountProfiling(mux)

	index := httptest.NewRecorder()
	mux.ServeHTTP(index, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/debug/pprof/", nil,
	))
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "goroutine") {
		t.Fatalf("pprof index = %d %.60q", index.Code, index.Body.String())
	}

	for _, path := range []string{
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/cmdline",
		"/debug/pprof/symbol",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(
			t.Context(), http.MethodGet, path, nil,
		))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rec.Code)
		}
	}
}
