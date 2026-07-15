package adminauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setupFormTestCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     setupFormCookieName,
		Value:    token,
		Path:     PathSetupPage,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

func setupFormTokenRequest(
	t *testing.T,
	token string,
	cookie *http.Cookie,
) *http.Request {
	t.Helper()
	form := url.Values{setupFormTokenField: {token}}
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSetupPage,
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", formContentType)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	return req
}

func TestSetupFormRequiresGETIssuedToken(t *testing.T) {
	useFastCredentialWork(t)
	service := testService(t)
	surface := htmlSurface(t, service)
	credentials := url.Values{
		usernameField: {"admin"},
		passwordField: {"correct-horse"},
	}

	hostileRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSetupPage,
		strings.NewReader(credentials.Encode()),
	)
	hostileRequest.Header.Set("Content-Type", formContentType)
	hostileRequest.Header.Set("Origin", "https://hostile.example")
	hostile := httptest.NewRecorder()
	surface.ServeHTTP(hostile, hostileRequest)
	if hostile.Code != http.StatusForbidden {
		t.Fatalf("hostile setup status = %d, want 403", hostile.Code)
	}
	if present, err := service.creds.exists(t.Context()); err != nil || present {
		t.Fatalf("hostile setup changed credentials: present=%v err=%v", present, err)
	}

	page := doRequest(surface, http.MethodGet, PathSetupPage, "")
	token := setupTokenFromBody(page.Body.String())
	cookie := setupCookieFromResponse(page)
	if page.Code != http.StatusOK || token == "" || cookie == nil {
		t.Fatalf("setup page = %d token=%q cookie=%#v", page.Code, token, cookie)
	}
	if cookie.Value != token || cookie.Path != PathSetupPage || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("setup cookie = %#v", cookie)
	}

	credentials.Set(setupFormTokenField, token)
	valid := postFormRaw(surface, PathSetupPage, credentials, cookie)
	if valid.Code != http.StatusSeeOther ||
		valid.Header().Get("Location") != PathLoginPage+"?notice=created" {
		t.Fatalf("valid setup = %d %q", valid.Code, valid.Header().Get("Location"))
	}
	cleared := setupCookieFromResponse(valid)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("setup cookie not cleared: %#v", cleared)
	}
}

func TestSetupFormRejectsExpiredAndMalformedTokens(t *testing.T) {
	base := time.Unix(1_900_000_000, 0)
	now := base
	service, err := New(testVault(t), Config{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	surface := htmlSurface(t, service)
	page := doRequest(surface, http.MethodGet, PathSetupPage, "")
	validToken := setupTokenFromBody(page.Body.String())
	validCookie := setupCookieFromResponse(page)
	if validToken == "" || validCookie == nil {
		t.Fatal("setup token was not issued")
	}

	future := strconv.FormatInt(base.Add(time.Hour).Unix(), 10)
	cases := map[string]struct {
		field  string
		cookie *http.Cookie
	}{
		"missing cookie": {field: validToken},
		"mismatched field": {
			field:  validToken + "x",
			cookie: validCookie,
		},
		"missing parts": {
			field:  "invalid",
			cookie: setupFormTestCookie("invalid"),
		},
		"invalid expiry": {
			field:  "nonce.invalid.signature",
			cookie: setupFormTestCookie("nonce.invalid.signature"),
		},
		"invalid encoding": {
			field:  "nonce." + future + ".%",
			cookie: setupFormTestCookie("nonce." + future + ".%"),
		},
		"wrong signature": {
			field:  "nonce." + future + ".AA",
			cookie: setupFormTestCookie("nonce." + future + ".AA"),
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if service.validSetupFormToken(setupFormTokenRequest(t, test.field, test.cookie)) {
				t.Fatal("invalid token accepted")
			}
		})
	}

	now = base.Add(setupFormTokenLifetime + time.Second)
	if service.validSetupFormToken(setupFormTokenRequest(t, validToken, validCookie)) {
		t.Fatal("expired token accepted")
	}
}

func TestSetupFormEntropyFailuresAreUnavailable(t *testing.T) {
	originalSigningRead := setupFormSigningKeyRead
	originalRandomRead := randRead
	t.Cleanup(func() {
		setupFormSigningKeyRead = originalSigningRead
		randRead = originalRandomRead
	})
	setupFormSigningKeyRead = func([]byte) (int, error) {
		return 0, errors.New("no entropy")
	}
	if _, err := New(testVault(t), Config{}); err == nil {
		t.Fatal("signing-key entropy failure accepted")
	}
	setupFormSigningKeyRead = func(buf []byte) (int, error) {
		return len(buf) - 1, nil
	}
	if _, err := New(testVault(t), Config{}); err == nil {
		t.Fatal("short signing-key read accepted")
	}
	setupFormSigningKeyRead = originalSigningRead

	service := testService(t)
	randRead = func([]byte) (int, error) {
		return 0, errors.New("no entropy")
	}
	rec := doRequest(htmlSurface(t, service), http.MethodGet, PathSetupPage, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("setup entropy failure status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != authPageCache ||
		rec.Header().Get("Content-Security-Policy") != authContentPolicy {
		t.Fatal("setup entropy failure lost auth page policy")
	}
}
