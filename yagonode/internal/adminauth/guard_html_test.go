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
	service := testService(t)
	if err := service.BootstrapFromEnv(context.Background(), "admin", "pw"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	surface := guardedSurface(t, service)
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

// TestGuardFirstRunRedirectsBrowserToSetup proves the guard sends a browser to
// the first-run setup page (not the login page) while no administrator exists,
// so a fresh node guides the operator to create the first admin.
func TestGuardFirstRunRedirectsBrowserToSetup(t *testing.T) {
	surface := guardedSurface(t, testService(t))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set(acceptHeader, "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != PathSetupPage {
		t.Fatalf("location = %q, want %q", loc, PathSetupPage)
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
