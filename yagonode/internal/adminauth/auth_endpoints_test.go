package adminauth

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestSetupCreatesFirstAdmin(t *testing.T) {
	service := testService(t)
	mux := mountAuth(t, service)

	rec := doRequest(mux, http.MethodPost, PathSetup, `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if present, _ := service.creds.exists(context.Background()); !present {
		t.Fatal("admin should exist after setup")
	}
}

func TestSetupRejectsSecondAdmin(t *testing.T) {
	mux := mountAuth(t, testService(t))
	if rec := doRequest(
		mux,
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"pw"}`,
	); rec.Code != http.StatusCreated {
		t.Fatalf("first setup = %d", rec.Code)
	}
	rec := doRequest(mux, http.MethodPost, PathSetup, `{"username":"other","password":"pw2"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup = %d, want 409", rec.Code)
	}
}

func TestSetupRejectsBadRequests(t *testing.T) {
	mux := mountAuth(t, testService(t))
	cases := map[string]struct {
		method string
		body   string
		want   int
	}{
		"non post":       {http.MethodGet, "", http.StatusMethodNotAllowed},
		"bad body":       {http.MethodPost, "{", http.StatusBadRequest},
		"missing fields": {http.MethodPost, `{"username":"admin"}`, http.StatusBadRequest},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doRequest(mux, tc.method, PathSetup, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestSetupSurfacesStoreError(t *testing.T) {
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	engine.buckets[adminCredentialsBucket][string(adminKey)] = []byte("{corrupt")
	rec := doRequest(
		mountAuth(t, service),
		http.MethodPost,
		PathSetup,
		`{"username":"a","password":"b"}`,
	)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestLoginSucceedsAndSetsCookie(t *testing.T) {
	service := testService(t)
	mux := mountAuth(t, service)
	if rec := doRequest(
		mux,
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"pw"}`,
	); rec.Code != http.StatusCreated {
		t.Fatalf("setup = %d", rec.Code)
	}

	rec := doRequest(mux, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d; body=%s", rec.Code, rec.Body.String())
	}
	cookie := cookieNamed(rec)
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly {
		t.Fatalf("session cookie = %#v", cookie)
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("SameSite = %v", cookie.SameSite)
	}
	var body loginResponse
	if err := decodeBody(rec, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Username != "admin" || body.CSRFToken == "" {
		t.Fatalf("login body = %#v", body)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	mux := mountAuth(t, testService(t))
	_ = doRequest(mux, http.MethodPost, PathSetup, `{"username":"admin","password":"pw"}`)
	rec := doRequest(mux, http.MethodPost, PathLogin, `{"username":"admin","password":"nope"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginRejectsWhenNoAdmin(t *testing.T) {
	mux := mountAuth(t, testService(t))
	rec := doRequest(mux, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginRejectsBadRequests(t *testing.T) {
	mux := mountAuth(t, testService(t))
	cases := map[string]struct {
		method string
		body   string
		want   int
	}{
		"non post":       {http.MethodGet, "", http.StatusMethodNotAllowed},
		"bad body":       {http.MethodPost, "{", http.StatusBadRequest},
		"missing fields": {http.MethodPost, `{"password":"pw"}`, http.StatusBadRequest},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doRequest(mux, tc.method, PathLogin, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestLoginRateLimits(t *testing.T) {
	mux := mountAuth(t, testService(t))
	_ = doRequest(mux, http.MethodPost, PathSetup, `{"username":"admin","password":"pw"}`)
	for range 3 {
		if rec := doRequest(
			mux,
			http.MethodPost,
			PathLogin,
			`{"username":"admin","password":"nope"}`,
		); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt = %d, want 401", rec.Code)
		}
	}
	rec := doRequest(mux, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

func TestLoginSurfacesVerifyError(t *testing.T) {
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	engine.buckets[adminCredentialsBucket][string(adminKey)] = []byte("{corrupt")
	rec := doRequest(
		mountAuth(t, service),
		http.MethodPost,
		PathLogin,
		`{"username":"admin","password":"pw"}`,
	)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestLoginSurfacesSessionError(t *testing.T) {
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	injectAdmin(t, engine, "admin", "pw")
	engine.putErr = errors.New("disk full")
	rec := doRequest(
		mountAuth(t, service),
		http.MethodPost,
		PathLogin,
		`{"username":"admin","password":"pw"}`,
	)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	service := testService(t)
	mux := mountAuth(t, service)
	_ = doRequest(mux, http.MethodPost, PathSetup, `{"username":"admin","password":"pw"}`)
	login := doRequest(mux, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	cookie := cookieNamed(login)

	rec := doRequest(mux, http.MethodPost, PathLogout, "", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	cleared := cookieNamed(rec)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("logout should clear the cookie: %#v", cleared)
	}
	if _, ok, _ := service.sessions.lookup(context.Background(), cookie.Value); ok {
		t.Fatal("session should be invalidated after logout")
	}
}

func TestLogoutWithoutCookieIsNoOp(t *testing.T) {
	mux := mountAuth(t, testService(t))
	rec := doRequest(mux, http.MethodPost, PathLogout, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestLogoutRejectsNonPost(t *testing.T) {
	mux := mountAuth(t, testService(t))
	rec := doRequest(mux, http.MethodGet, PathLogout, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestLogoutSurfacesDeleteError(t *testing.T) {
	engine := newScriptedEngine()
	service, err := New(scriptedVault(t, engine), Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	injectAdmin(t, engine, "admin", "pw")
	mux := mountAuth(t, service)
	login := doRequest(mux, http.MethodPost, PathLogin, `{"username":"admin","password":"pw"}`)
	cookie := cookieNamed(login)

	engine.deleteErr = errors.New("delete failed")
	rec := doRequest(mux, http.MethodPost, PathLogout, "", cookie)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestClientIPFallsBackWhenNoPort(t *testing.T) {
	withPort := &http.Request{RemoteAddr: "203.0.113.7:5555"}
	if got := clientIP(withPort); got != "203.0.113.7" {
		t.Fatalf("clientIP with port = %q, want 203.0.113.7", got)
	}
	withoutPort := &http.Request{RemoteAddr: "203.0.113.7"}
	if got := clientIP(withoutPort); got != "203.0.113.7" {
		t.Fatalf("clientIP without port = %q, want 203.0.113.7", got)
	}
}

func TestSessionEndpointRejectsNonGet(t *testing.T) {
	mux := mountAuth(t, testService(t))
	rec := doRequest(mux, http.MethodPost, PathSession, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSessionEndpointRejectsWithoutContext(t *testing.T) {
	mux := mountAuth(t, testService(t))
	rec := doRequest(mux, http.MethodGet, PathSession, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
