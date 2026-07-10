package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPEndpointMetricsObservesUnmatchedEndpoint(t *testing.T) {
	metrics := NewHTTPEndpointMetrics()

	metrics.Observe("", http.StatusNotFound, 2*time.Second)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `endpoint="unmatched"`) {
		t.Fatalf("metrics missing unmatched endpoint label:\n%s", body)
	}
	if !strings.Contains(body, `code="404"`) {
		t.Fatalf("metrics missing status label:\n%s", body)
	}
}

func TestHTTPEndpointMetricsRegistry(t *testing.T) {
	metrics := NewHTTPEndpointMetrics()

	if metrics.Registry() == nil {
		t.Fatal("registry is nil")
	}
}

func TestObserveExemplarPinsTraceOnSampledRequest(t *testing.T) {
	metrics := NewHTTPEndpointMetrics()

	metrics.ObserveExemplar("/search", http.StatusOK, 5*time.Millisecond, "trace-abc")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `endpoint="/search"`) {
		t.Fatalf("metrics missing the observed endpoint label:\n%s", body)
	}
}

func TestObserveExemplarFallsBackWithoutTraceID(t *testing.T) {
	metrics := NewHTTPEndpointMetrics()

	metrics.ObserveExemplar("", http.StatusInternalServerError, time.Second, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `endpoint="unmatched"`) {
		t.Fatalf("metrics missing the unmatched fallback label:\n%s", body)
	}
}
