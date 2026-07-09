package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpsMuxServesHealthAndMetrics(t *testing.T) {
	served := false
	mux := newOpsMux(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	ctx := context.Background()
	health := httptest.NewRecorder()
	mux.ServeHTTP(health, httptest.NewRequestWithContext(ctx, "GET", pathHealth, nil))
	if health.Code != http.StatusOK {
		t.Errorf("health status = %d", health.Code)
	}

	metrics := httptest.NewRecorder()
	mux.ServeHTTP(metrics, httptest.NewRequestWithContext(ctx, "GET", pathMetrics, nil))
	if metrics.Code != http.StatusOK || !served {
		t.Errorf("metrics status = %d, served = %v", metrics.Code, served)
	}
}
