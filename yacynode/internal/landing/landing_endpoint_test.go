package landing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingLandingWriter struct {
	header http.Header
}

func (w *failingLandingWriter) Header() http.Header {
	return w.header
}

func (w *failingLandingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *failingLandingWriter) WriteHeader(int) {}

func TestNewEndpointReturnsLandingHandler(t *testing.T) {
	handler := NewEndpoint()

	if _, ok := handler.(landingEndpoint); !ok {
		t.Fatalf("handler type = %T, want landingEndpoint", handler)
	}
}

func TestLandingEndpointServesHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	landingEndpoint{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != landingPageContentType {
		t.Errorf("content type = %q, want %q", got, landingPageContentType)
	}
	body := rec.Body.String()
	for _, want := range []string{"alpha", "RWI", "github.com/D4rk4/yago/issues"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestLandingEndpointServesHead(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/", nil)

	landingEndpoint{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != landingPageContentType {
		t.Errorf("content type = %q, want %q", got, landingPageContentType)
	}
}

func TestLandingEndpointLogsWriteFailure(t *testing.T) {
	writer := &failingLandingWriter{header: http.Header{}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	landingEndpoint{}.ServeHTTP(writer, req)

	if got := writer.Header().Get("Content-Type"); got != landingPageContentType {
		t.Errorf("content type = %q, want %q", got, landingPageContentType)
	}
}

func TestLandingEndpointRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)

	landingEndpoint{}.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("allow = %q, want %q", got, http.MethodGet)
	}
}
