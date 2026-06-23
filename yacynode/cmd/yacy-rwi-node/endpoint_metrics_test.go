package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpointMetricsExposesPerEndpointSeries(t *testing.T) {
	metrics := newEndpointMetrics()

	metrics.observe("/yacy/transferRWI.html", http.StatusOK, 2*time.Millisecond)
	metrics.observe("/yacy/transferRWI.html", http.StatusOK, 4*time.Millisecond)
	metrics.observe("/yacy/transferRWI.html", http.StatusBadRequest, time.Millisecond)
	metrics.observe("", http.StatusNotFound, time.Millisecond)

	body := scrapeMetrics(t, metrics)

	for _, want := range []string{
		`http_requests_total{code="200",endpoint="/yacy/transferRWI.html"} 2`,
		`http_requests_total{code="400",endpoint="/yacy/transferRWI.html"} 1`,
		`http_requests_total{code="404",endpoint="unmatched"} 1`,
		`http_request_duration_seconds_count{endpoint="/yacy/transferRWI.html"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q\n%s", want, body)
		}
	}
}

func TestEndpointMetricsCountsThroughMiddleware(t *testing.T) {
	metrics := newEndpointMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("/yacy/hello.html", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := instrumentHTTP(metrics, mux)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "/yacy/hello.html", nil,
	)
	handler.ServeHTTP(rec, req)

	body := scrapeMetrics(t, metrics)
	want := `http_request_duration_seconds_count{endpoint="/yacy/hello.html"} 1`
	if !strings.Contains(body, want) {
		t.Errorf("metrics output missing %q\n%s", want, body)
	}
}

func scrapeMetrics(t *testing.T, metrics *endpointMetrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, pathMetrics, nil,
	)
	metrics.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", rec.Code)
	}

	return rec.Body.String()
}
