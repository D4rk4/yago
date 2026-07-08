package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpsMux(t *testing.T) {
	mux := newOpsMux(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("metrics"))
	}))

	health := httptest.NewRecorder()
	mux.ServeHTTP(
		health,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, pathHealth, nil),
	)
	if health.Code != http.StatusOK {
		t.Fatalf("health code = %d", health.Code)
	}

	metrics := httptest.NewRecorder()
	mux.ServeHTTP(
		metrics,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, pathMetrics, nil),
	)
	if metrics.Body.String() != "metrics" {
		t.Fatalf("metrics body = %q", metrics.Body.String())
	}
}
