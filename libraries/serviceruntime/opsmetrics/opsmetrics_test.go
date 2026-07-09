package opsmetrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/opsmetrics"
)

func TestNewMuxServesMetrics(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("served"))
	})
	mux := opsmetrics.NewMux(handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "served" {
		t.Errorf("metrics = %d %q", rec.Code, rec.Body.String())
	}
}

func TestNewMuxRejectsUnknownPath(t *testing.T) {
	mux := opsmetrics.NewMux(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", rec.Code)
	}
}
