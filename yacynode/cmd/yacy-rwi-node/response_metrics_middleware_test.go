package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacynode/internal/metrics"
)

func TestInstrumentHTTPCountsThroughMiddleware(t *testing.T) {
	endpoints := metrics.NewHTTPEndpointMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("/yacy/hello.html", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := instrumentHTTP(endpoints, mux)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "/yacy/hello.html", nil,
	)
	handler.ServeHTTP(rec, req)

	scrape := httptest.NewRecorder()
	scrapeReq, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, pathMetrics, nil,
	)
	endpoints.Handler().ServeHTTP(scrape, scrapeReq)

	want := `http_request_duration_seconds_count{endpoint="/yacy/hello.html"} 1`
	if !strings.Contains(scrape.Body.String(), want) {
		t.Errorf("metrics output missing %q\n%s", want, scrape.Body.String())
	}
}
