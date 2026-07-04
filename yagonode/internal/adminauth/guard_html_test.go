package adminauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGuardRedirectsBrowserToLogin(t *testing.T) {
	surface := guardedSurface(t, testService(t))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set(acceptHeader, "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != PathLoginPage {
		t.Fatalf("location = %q, want %q", loc, PathLoginPage)
	}
}

func TestGuardAcceptsCSRFFormField(t *testing.T) {
	service := testService(t)
	surface := guardedSurface(t, service)
	cookie, csrf := loginThroughGuard(t, surface)

	rec := postThroughGuard(surface, url.Values{csrfFormField: {csrf}}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestGuardRejectsWrongCSRFFormField(t *testing.T) {
	service := testService(t)
	surface := guardedSurface(t, service)
	cookie, _ := loginThroughGuard(t, surface)

	rec := postThroughGuard(surface, url.Values{csrfFormField: {"wrong"}}, cookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func postThroughGuard(
	handler http.Handler,
	form url.Values,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/protected",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set(contentType, formContentType)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}
