package proxyintake

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/renderedpage"
)

type stubRenderer struct {
	page renderedpage.Page
	err  error
}

func (s stubRenderer) Render(context.Context, string) (renderedpage.Page, error) {
	return s.page, s.err
}

func TestServeHTTPRefusesConnect(t *testing.T) {
	handler := New(stubRenderer{})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodConnect,
		"http://example.com/",
		nil,
	)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestServeHTTPRejectsNonAbsoluteRequest(t *testing.T) {
	handler := New(stubRenderer{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/path", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestServeHTTPReturnsRenderedPage(t *testing.T) {
	handler := New(stubRenderer{page: renderedpage.Page{
		StatusCode:  http.StatusOK,
		ContentType: "text/html",
		Body:        []byte("<html>hi</html>"),
	}})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/",
		nil,
	)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "<html>hi</html>" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html" {
		t.Fatalf("content-type = %q", got)
	}
}

func TestServeHTTPFailsOnDeadlineExceeded(t *testing.T) {
	handler := New(stubRenderer{err: context.DeadlineExceeded})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/",
		nil,
	)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
}

func TestServeHTTPFailsOnRenderError(t *testing.T) {
	handler := New(stubRenderer{err: errors.New("browser unreachable")})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/",
		nil,
	)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
