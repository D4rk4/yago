package siteicon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMountServesCurrentAndLegacyPaths(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	Mount(mux)
	for _, path := range []string{Path, LegacyPath} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		mux.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, recorder.Code)
		}
		if recorder.Header().Get("Content-Type") != "image/svg+xml" {
			t.Fatalf("%s content type = %q", path, recorder.Header().Get("Content-Type"))
		}
		if recorder.Header().Get("Cache-Control") != siteIconCachePolicy {
			t.Fatalf("%s cache policy = %q", path, recorder.Header().Get("Cache-Control"))
		}
		if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s missing nosniff", path)
		}
		if !strings.Contains(recorder.Body.String(), "<svg") {
			t.Fatalf("%s body is not an SVG", path)
		}
	}
}

func TestMountRejectsPost(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	Mount(mux)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, Path, nil)
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}
