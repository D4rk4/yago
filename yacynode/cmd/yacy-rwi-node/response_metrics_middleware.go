package main

import (
	"net/http"
	"time"
)

func instrumentHTTP(metrics *endpointMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		metrics.observe(r.Pattern, recorder.status, time.Since(started))
	})
}
