package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

// TestInstrumentHTTPCountsThroughMiddleware pins that one request records
// exactly one latency observation and one request count on both the sampled
// and the unsampled path. The sampled path takes the exemplar branch, which
// must record a single histogram observation carrying the exemplar rather than
// a plain observation plus an exemplar — the double-count that made this test
// flake ~1-in-256, whenever a headerless request drew a freshly-sampled trace.
func TestInstrumentHTTPCountsThroughMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name        string
		traceparent string
	}{
		{name: "sampled", traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{name: "unsampled", traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoints := metrics.NewHTTPEndpointMetrics()
			mux := http.NewServeMux()
			mux.HandleFunc("/yacy/hello.html", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := instrumentHTTP(endpoints, mux)

			req, _ := http.NewRequestWithContext(
				context.Background(), http.MethodGet, "/yacy/hello.html", nil,
			)
			req.Header.Set(tracectx.Header, tc.traceparent)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			scrape := httptest.NewRecorder()
			scrapeReq, _ := http.NewRequestWithContext(
				context.Background(), http.MethodGet, pathMetrics, nil,
			)
			endpoints.Handler().ServeHTTP(scrape, scrapeReq)
			body := scrape.Body.String()

			for _, want := range []string{
				`http_request_duration_seconds_count{endpoint="/yacy/hello.html"} 1`,
				`http_requests_total{code="200",endpoint="/yacy/hello.html"} 1`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("metrics output missing %q\n%s", want, body)
				}
			}
		})
	}
}
