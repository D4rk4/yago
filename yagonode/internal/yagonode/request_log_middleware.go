package yagonode

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

const (
	requestHandledMessage = "http request handled"
	requestFailedMessage  = "http request failed"
)

func logHTTPRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		attrs := []slog.Attr{
			slog.String("method", requestLogMethod(r.Method)),
			slog.String("path", route),
			slog.Int("status", recorder.status),
			slog.Int64("durationMs", time.Since(started).Milliseconds()),
			tracectx.ServerSpanAttribute(r.Context()),
		}
		if recorder.status >= http.StatusBadRequest {
			slog.LogAttrs(r.Context(), slog.LevelWarn, requestFailedMessage, attrs...)

			return
		}
		slog.LogAttrs(r.Context(), slog.LevelDebug, requestHandledMessage, attrs...)
	})
}
