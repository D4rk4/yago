package adminauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func htmlSurface(t *testing.T, service *Service) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	MountHTML(mux, service)

	return mux
}

func postForm(
	handler http.Handler,
	path string,
	form url.Values,
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	preparedForm := form
	preparedCookies := cookies
	if path == PathSetupPage {
		setup := doRequest(handler, http.MethodGet, PathSetupPage, "")
		if setup.Code == http.StatusOK {
			if token := setupTokenFromBody(setup.Body.String()); token != "" {
				preparedForm = make(url.Values, len(form)+1)
				for key, values := range form {
					preparedForm[key] = append([]string(nil), values...)
				}
				preparedForm.Set(setupFormTokenField, token)
			}
			if cookie := setupCookieFromResponse(setup); cookie != nil {
				preparedCookies = append(append([]*http.Cookie{}, cookies...), cookie)
			}
		}
	}

	return postFormRaw(handler, path, preparedForm, preparedCookies...)
}

func postFormRaw(
	handler http.Handler,
	path string,
	form url.Values,
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		path,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", formContentType)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func setupTokenFromBody(body string) string {
	const prefix = `name="setup_token" value="`
	start := strings.Index(body, prefix)
	if start < 0 {
		return ""
	}
	value := body[start+len(prefix):]
	end := strings.IndexByte(value, '"')
	if end < 0 {
		return ""
	}

	return value[:end]
}

func setupCookieFromResponse(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == setupFormCookieName {
			return cookie
		}
	}

	return nil
}

func TestLoginPageRendersForm(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "operator", "correct-horse")
	rec := doRequest(htmlSurface(t, service), http.MethodGet, PathLoginPage, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/admin/login"`) ||
		!strings.Contains(body, `name="username"`) {
		t.Fatalf("login form missing fields: %s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestLoginPageShowsError(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "operator", "correct-horse")
	rec := doRequest(htmlSurface(t, service), http.MethodGet, PathLoginPage+"?error=invalid", "")
	if !strings.Contains(rec.Body.String(), "Invalid username or password.") {
		t.Fatalf("expected error message, got %s", rec.Body.String())
	}
}

// TestLoginPageFirstRunRedirectsToSetup proves that while no administrator exists
// the login page routes the operator to the first-run setup page instead of
// stranding them on a login form for an account that has not been created yet.
func TestLoginPageFirstRunRedirectsToSetup(t *testing.T) {
	service, _ := scriptedService(t)
	rec := doRequest(htmlSurface(t, service), http.MethodGet, PathLoginPage, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != PathSetupPage {
		t.Fatalf("location = %q, want %q", loc, PathSetupPage)
	}
}

func TestLoginFormSuccessRedirectsToOverview(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "operator", "correct-horse")
	rec := postForm(htmlSurface(t, service), PathLoginPage, url.Values{
		"username": {"operator"}, "password": {"correct-horse"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != overviewRedirect {
		t.Fatalf("location = %q, want %q", loc, overviewRedirect)
	}
	if cookieNamed(rec) == nil {
		t.Fatal("expected a session cookie")
	}
}

func TestLoginFormInvalidRedirectsWithError(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "correct-horse")
	rec := postForm(htmlSurface(t, service), PathLoginPage, url.Values{
		"username": {"admin"}, "password": {"wrong"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != PathLoginPage+"?error=invalid" {
		t.Fatalf("location = %q", loc)
	}
	if cookieNamed(rec) != nil {
		t.Fatal("did not expect a session cookie")
	}
}

func TestLoginFormThrottled(t *testing.T) {
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{
		SessionTTL:       time.Hour,
		LoginMaxFailures: 1,
		LoginWindow:      time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	injectAdmin(t, engine, "admin", "correct-horse")
	surface := htmlSurface(t, service)

	first := postForm(
		surface,
		PathLoginPage,
		url.Values{"username": {"admin"}, "password": {"wrong"}},
	)
	if loc := first.Header().Get("Location"); loc != PathLoginPage+"?error=invalid" {
		t.Fatalf("first location = %q, want invalid", loc)
	}
	second := postForm(
		surface,
		PathLoginPage,
		url.Values{"username": {"admin"}, "password": {"wrong"}},
	)
	if loc := second.Header().Get("Location"); loc != PathLoginPage+"?error=throttled" {
		t.Fatalf("second location = %q, want throttled", loc)
	}
}

func TestSetupPageRendersWhenNoAdmin(t *testing.T) {
	service, _ := scriptedService(t)
	rec := doRequest(htmlSurface(t, service), http.MethodGet, PathSetupPage, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `action="/admin/setup"`) {
		t.Fatalf("setup form missing: %s", rec.Body.String())
	}
}

func TestSetupPageRedirectsWhenAdminExists(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "pw")
	rec := doRequest(htmlSurface(t, service), http.MethodGet, PathSetupPage, "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != PathLoginPage {
		t.Fatalf("location = %q", loc)
	}
}

func TestSetupFormCreatesAdmin(t *testing.T) {
	service, _ := scriptedService(t)
	rec := postForm(htmlSurface(t, service), PathSetupPage, url.Values{
		"username": {"admin"}, "password": {"correct-horse"},
	})
	if loc := rec.Header().Get("Location"); loc != PathLoginPage+"?notice=created" {
		t.Fatalf("location = %q, want created", loc)
	}
	present, err := service.creds.exists(context.Background())
	if err != nil || !present {
		t.Fatalf("admin not created: present=%v err=%v", present, err)
	}
}

func TestSetupFormRejectsMissingFields(t *testing.T) {
	service, _ := scriptedService(t)
	rec := postForm(htmlSurface(t, service), PathSetupPage, url.Values{"username": {"admin"}})
	if loc := rec.Header().Get("Location"); loc != PathSetupPage+"?error=missing" {
		t.Fatalf("location = %q, want missing", loc)
	}
}

func TestLogoutFormClearsSession(t *testing.T) {
	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "correct-horse")
	surface := htmlSurface(t, service)

	login := postForm(surface, PathLoginPage, url.Values{
		"username": {"admin"}, "password": {"correct-horse"},
	})
	cookie := cookieNamed(login)
	if cookie == nil {
		t.Fatal("no session cookie from login")
	}

	rec := postForm(surface, PathLogoutForm, url.Values{}, cookie)
	if loc := rec.Header().Get("Location"); loc != PathLoginPage {
		t.Fatalf("location = %q, want clean login page", loc)
	}
	cleared := cookieNamed(rec)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("session cookie not cleared: %#v", cleared)
	}
	page := doRequest(surface, http.MethodGet, rec.Header().Get("Location"), "")
	if strings.Contains(page.Body.String(), "signed out") {
		t.Fatalf("logout rendered a session notice: %s", page.Body.String())
	}
}
