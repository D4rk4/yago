package main

import (
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/metrics"
)

func instrumentHTTP(endpoints *metrics.HTTPEndpointMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		endpoints.Observe(r.Pattern, recorder.status, time.Since(started))
	})
}
