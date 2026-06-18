package infrastructure

import (
	"log/slog"
	"net/http"
	"time"
)

const (
	requestHandledMessage = "http request handled"
	requestFailedMessage  = "http request failed"
)

func LogHTTPRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		}
		if recorder.status >= http.StatusBadRequest {
			slog.WarnContext(r.Context(), requestFailedMessage, attrs...)
			return
		}
		slog.DebugContext(r.Context(), requestHandledMessage, attrs...)
	})
}
