package adminauth

import (
	"net/http"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sessionFromContext(r.Context()); !ok {
			w.WriteHeader(http.StatusTeapot)

			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func guardedSurface(t *testing.T, service *Service) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	Mount(mux, service)
	mux.Handle("/protected", okHandler())

	return service.Guard([]string{PathLogin, PathSetup}, mux)
}

func loginThroughGuard(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	if rec := doRequest(
		handler,
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"pw"}`,
	); rec.Code != http.StatusCreated {
		t.Fatalf("setup through guard = %d", rec.Code)
	}
	rec := doRequest(handler, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login through guard = %d", rec.Code)
	}
	var body loginResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	return cookieNamed(rec), body.CSRFToken
}

func TestGuardPassesExemptPaths(t *testing.T) {
	service := testService(t)
	handler := service.Guard([]string{PathSetup}, mountAuth(t, service))
	rec := doRequest(handler, http.MethodPost, PathSetup, `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("exempt path = %d, want 201", rec.Code)
	}
}

func TestGuardRejectsWithoutCookie(t *testing.T) {
	rec := doRequest(guardedSurface(t, testService(t)), http.MethodGet, "/protected", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGuardRejectsUnknownSession(t *testing.T) {
	surface := guardedSurface(t, testService(t))
	//nolint:gosec // G124: client-supplied test cookie, not a server Set-Cookie.
	stale := &http.Cookie{ // nosemgrep: go.lang.security.audit.net.cookie-missing-httponly.cookie-missing-httponly, go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- client-supplied test cookie, not a server Set-Cookie.
		Name:  sessionCookieName,
		Value: "stale-token",
	}
	rec := doRequest(surface, http.MethodGet, "/protected", "", stale)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGuardAllowsSafeMethodWithSession(t *testing.T) {
	service := testService(t)
	surface := guardedSurface(t, service)
	cookie, _ := loginThroughGuard(t, surface)
	rec := doRequest(surface, http.MethodGet, "/protected", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGuardRequiresCSRFForUnsafeMethod(t *testing.T) {
	service := testService(t)
	surface := guardedSurface(t, service)
	cookie, csrf := loginThroughGuard(t, surface)

	missing := doRequest(surface, http.MethodPost, "/protected", "", cookie)
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing csrf = %d, want 403", missing.Code)
	}

	req := doRequestWithCSRF(surface, http.MethodPost, "/protected", "wrong", cookie)
	if req.Code != http.StatusForbidden {
		t.Fatalf("wrong csrf = %d, want 403", req.Code)
	}

	ok := doRequestWithCSRF(surface, http.MethodPost, "/protected", csrf, cookie)
	if ok.Code != http.StatusOK {
		t.Fatalf("valid csrf = %d, want 200", ok.Code)
	}
}

func TestGuardServesSessionEndpoint(t *testing.T) {
	service := testService(t)
	surface := guardedSurface(t, service)
	cookie, _ := loginThroughGuard(t, surface)

	rec := doRequest(surface, http.MethodGet, PathSession, "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("session = %d; body=%s", rec.Code, rec.Body.String())
	}
	var body sessionResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Username != "admin" {
		t.Fatalf("session body = %#v", body)
	}
}

func TestGuardSurfacesLookupError(t *testing.T) {
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	injectAdmin(t, engine, "admin", "pw")
	surface := guardedSurface(t, service)
	login := doRequest(surface, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	cookie := cookieNamed(login)

	engine.buckets[adminSessionsBucket][hashToken(cookie.Value)] = []byte("{corrupt")
	rec := doRequest(surface, http.MethodGet, "/protected", "", cookie)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
