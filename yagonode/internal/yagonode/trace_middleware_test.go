package yagonode

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

func TestInstrumentHTTPStartsServerSpan(t *testing.T) {
	var seen tracectx.Trace
	var seenServerSpanID string
	var hasServerSpanID bool
	handler := instrumentHTTP(
		metrics.NewHTTPEndpointMetrics(),
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen, _ = tracectx.FromContext(r.Context())
			seenServerSpanID, hasServerSpanID = tracectx.ServerSpanIDFromContext(r.Context())
		}),
	)

	inbound := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/yacysearch.json", nil,
	)
	inbound.Header.Set(
		tracectx.Header,
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	)
	handler.ServeHTTP(httptest.NewRecorder(), inbound)
	if seen.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || !seen.Sampled {
		t.Fatalf("inbound trace not adopted: %+v", seen)
	}
	if seen.SpanID == "00f067aa0ba902b7" {
		t.Fatal("server reused the caller's parent span ID")
	}
	if !hasServerSpanID || seenServerSpanID != seen.SpanID {
		t.Fatalf(
			"server span ID = %q, ok=%v; want %q",
			seenServerSpanID,
			hasServerSpanID,
			seen.SpanID,
		)
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/yacysearch.json", nil,
	))
	if len(seen.TraceID) != 32 || seen.TraceID == "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("fresh trace missing: %+v", seen)
	}
	if !hasServerSpanID || seenServerSpanID != seen.SpanID {
		t.Fatalf(
			"fresh server span ID = %q, ok=%v; want %q",
			seenServerSpanID,
			hasServerSpanID,
			seen.SpanID,
		)
	}
}

func TestObserveExemplarTolerates(t *testing.T) {
	endpoints := metrics.NewHTTPEndpointMetrics()
	endpoints.ObserveExemplar("", 0, 0, "abc")
	endpoints.ObserveExemplar("/x", http.StatusOK, 100, "4bf92f3577b34da6a3ce929d0e0e4736")
	endpoints.ObserveExemplar("/y", http.StatusOK, 50, "")
}
