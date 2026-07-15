package httpguard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type wireResponseFailureWriter struct {
	failure error
	header  http.Header
}

func (w *wireResponseFailureWriter) Header() http.Header {
	return w.header
}

func (w *wireResponseFailureWriter) Write([]byte) (int, error) {
	return 0, w.failure
}

func (w *wireResponseFailureWriter) WriteHeader(int) {}

func captureWireResponseLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &output
}

func TestWireResponseClientDisconnectsLogAtDebug(t *testing.T) {
	for _, disconnect := range []error{syscall.EPIPE, syscall.ECONNRESET} {
		output := captureWireResponseLogs(t)
		writer := &wireResponseFailureWriter{
			failure: fmt.Errorf("write tcp: %w", disconnect),
			header:  http.Header{},
		}

		writeWireMessage(context.Background(), writer, yagomodel.Message{"a": "b"})

		logged := output.String()
		for _, want := range []string{
			`"level":"DEBUG"`,
			`"msg":"` + msgWireResponseWriteFailed + `"`,
			`"reason":"` + wireResponseClientDisconnectReason + `"`,
		} {
			if !strings.Contains(logged, want) {
				t.Fatalf("disconnect %v log = %q, want %q", disconnect, logged, want)
			}
		}
		if strings.Contains(logged, `"level":"WARN"`) {
			t.Fatalf("disconnect %v log = %q, unexpected WARN", disconnect, logged)
		}
	}
}

func TestWireResponseOtherFailuresRemainWarn(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() context.Context
	}{
		{name: "live context", ctx: context.Background},
		{
			name: "canceled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := captureWireResponseLogs(t)
			writer := &wireResponseFailureWriter{
				failure: errors.New("write failed"),
				header:  http.Header{},
			}

			writeWireMessage(test.ctx(), writer, yagomodel.Message{"a": "b"})

			logged := output.String()
			for _, want := range []string{
				`"level":"WARN"`,
				`"msg":"` + msgWireResponseWriteFailed + `"`,
				`"error":"write response text: write failed"`,
			} {
				if !strings.Contains(logged, want) {
					t.Fatalf("log = %q, want %q", logged, want)
				}
			}
		})
	}
}
