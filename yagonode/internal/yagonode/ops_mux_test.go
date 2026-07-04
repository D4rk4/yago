package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func TestMetricsHandlerHonoursToggle(t *testing.T) {
	endpoints := metrics.NewHTTPEndpointMetrics()

	if metricsHandler(endpoints, false) != nil {
		t.Fatal("disabled metrics must yield no handler")
	}
	if metricsHandler(endpoints, true) == nil {
		t.Fatal("enabled metrics must yield a handler")
	}
}

func requestMetrics(t *testing.T, mux *http.ServeMux) int {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, pathMetrics, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec.Code
}

func TestNewOpsMuxOmitsMetricsWhenNil(t *testing.T) {
	mux := newOpsMux(nil, nil, nil, nil, nil)

	if got := requestMetrics(t, mux); got != http.StatusNotFound {
		t.Fatalf("GET %s = %d, want 404 when metrics disabled", pathMetrics, got)
	}
}

func TestNewOpsMuxServesMetricsWhenPresent(t *testing.T) {
	endpoints := metrics.NewHTTPEndpointMetrics()
	mux := newOpsMux(endpoints.Handler(), nil, nil, nil, nil)

	if got := requestMetrics(t, mux); got != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", pathMetrics, got)
	}
}
