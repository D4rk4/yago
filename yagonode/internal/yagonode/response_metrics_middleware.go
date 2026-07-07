package yagonode

import (
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

// instrumentHTTP measures every request and roots its trace context (OPS-10):
// a valid inbound traceparent is adopted (the caller correlates us with its
// own trace), otherwise a sampled-1/64 trace starts here; sampled traces
// attach their ID as an exemplar on the latency histogram.
func instrumentHTTP(endpoints *metrics.HTTPEndpointMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		trace, ok := tracectx.Parse(r.Header.Get(tracectx.Header))
		if !ok {
			trace = tracectx.New()
		}
		r = r.WithContext(tracectx.WithContext(r.Context(), trace))
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		elapsed := time.Since(started)
		endpoints.Observe(r.Pattern, recorder.status, elapsed)
		if trace.Sampled {
			endpoints.ObserveExemplar(r.Pattern, elapsed, trace.TraceID)
		}
	})
}
