package httpguard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type failingResponseWriter struct {
	header http.Header
}

func (w *failingResponseWriter) Header() http.Header {
	return w.header
}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *failingResponseWriter) WriteHeader(int) {}

func TestWriteRawResponseLogsWriteFailure(t *testing.T) {
	writer := &failingResponseWriter{header: http.Header{}}

	writeRawResponse(context.Background(), writer, RawResponse{
		ContentType: "text/plain",
		Body:        "body",
	})

	if got := writer.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestWriteWireMessageLogsWriteFailure(t *testing.T) {
	writer := &failingResponseWriter{header: http.Header{}}

	writeWireMessage(context.Background(), writer, yagomodel.Message{"a": "b"})

	if got := writer.Header().Get("Content-Type"); got != wireContentType {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestWriteResponseTextReturnsWriteError(t *testing.T) {
	writer := &failingResponseWriter{header: http.Header{}}

	if err := writeResponseText(writer, "body"); err == nil {
		t.Fatal("expected write error")
	}
}

func TestClientAddressResolverHandlesRemoteAddrWithoutPort(t *testing.T) {
	resolver := NewClientAddressResolver(nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9"

	if got := resolver.Resolve(req); got != "203.0.113.9" {
		t.Fatalf("addr = %q", got)
	}
}
