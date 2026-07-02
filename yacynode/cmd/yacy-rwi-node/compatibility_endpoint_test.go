package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/compatibility"
)

func TestCompatibilityEndpointReturnsCatalog(t *testing.T) {
	endpoint := compatibilityEndpoint{
		now: func() time.Time {
			return time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathCompatibility, nil)
	endpoint.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"path"`)) ||
		bytes.Contains(rec.Body.Bytes(), []byte(`"Path"`)) {
		t.Fatalf("response uses unstable JSON field names: %s", rec.Body.String())
	}

	var got compatibilityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.GeneratedAt != "2026-07-02T09:00:00Z" ||
		len(got.Surfaces) == 0 ||
		len(got.Counts) == 0 {
		t.Fatalf("response = %#v", got)
	}
	if !slices.ContainsFunc(got.Surfaces, func(surface compatibility.Surface) bool {
		return surface.Path == pathCompatibility && surface.State == compatibility.Implemented
	}) {
		t.Fatalf("compatibility surface missing from response: %#v", got.Surfaces)
	}
}

func TestCompatibilityEndpointRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, pathCompatibility, nil)
	newCompatibilityEndpoint().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", rec.Header().Get("Allow"))
	}
}
