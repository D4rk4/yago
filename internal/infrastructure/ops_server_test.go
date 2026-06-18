package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpsMuxHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, PathHealth, nil)
	NewOpsMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("health status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestOpsMuxMetrics(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, PathMetrics, nil)
	NewOpsMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("metrics status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("metrics content-type = %q", ct)
	}
}
