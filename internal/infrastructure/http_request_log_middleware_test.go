package infrastructure

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogHTTPRequestsLogsSuccessfulRequestAtDebug(t *testing.T) {
	var buf bytes.Buffer
	restore := captureRequestLogs(t, &buf, slog.LevelDebug)

	handler := LogHTTPRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ok", nil),
	)

	restore()
	got := buf.String()
	if !strings.Contains(got, `msg="http request handled"`) {
		t.Fatalf("log = %q, want handled message", got)
	}
	if strings.Contains(got, `msg="http request failed"`) {
		t.Fatalf("log = %q, did not want failed message", got)
	}
}

func TestLogHTTPRequestsLogsFailedRequestAtWarn(t *testing.T) {
	var buf bytes.Buffer
	restore := captureRequestLogs(t, &buf, slog.LevelWarn)

	handler := LogHTTPRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/bad", nil),
	)

	restore()
	got := buf.String()
	if !strings.Contains(got, `msg="http request failed"`) {
		t.Fatalf("log = %q, want failed message", got)
	}
	if !strings.Contains(got, "status=400") {
		t.Fatalf("log = %q, want status", got)
	}
}

func captureRequestLogs(tb testing.TB, buf *bytes.Buffer, level slog.Level) func() {
	tb.Helper()

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level})))

	return func() {
		slog.SetDefault(previous)
	}
}
