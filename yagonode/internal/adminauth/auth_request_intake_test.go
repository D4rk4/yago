package adminauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type authBodyProbe struct {
	reads atomic.Int64
}

func (p *authBodyProbe) Read([]byte) (int, error) {
	p.reads.Add(1)

	return 0, errors.New("body read")
}

func (p *authBodyProbe) Close() error { return nil }

func serveAuthBody(
	handler http.Handler,
	path, contentType string,
	body io.ReadCloser,
	contentLength int64,
) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil)
	req.Body = body
	req.ContentLength = contentLength
	req.RemoteAddr = "192.0.2.1:1234"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func oversizedCredentialsJSON() string {
	return `{"username":"admin","password":"` +
		strings.Repeat("x", int(maximumAuthRequestBodyBytes)) + `"}`
}

func oversizedCredentialsForm() string {
	return url.Values{
		usernameField: {"admin"},
		passwordField: {strings.Repeat("x", int(maximumAuthRequestBodyBytes))},
	}.Encode()
}

func TestAuthJSONRejectsOversizedAndTrailingBodies(t *testing.T) {
	setup := mountAuth(t, testService(t))
	if rec := doRequest(
		setup,
		http.MethodPost,
		PathSetup,
		oversizedCredentialsJSON(),
	); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized setup status = %d", rec.Code)
	}
	if rec := doRequest(
		setup,
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"pw"} {}`,
	); rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing setup status = %d", rec.Code)
	}

	service, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "pw")
	login := mountAuth(t, service)
	for name, contentLength := range map[string]int64{
		"declared": int64(len(oversizedCredentialsJSON())),
		"streamed": -1,
	} {
		t.Run(name, func(t *testing.T) {
			rec := serveAuthBody(
				login,
				PathLogin,
				"application/json",
				io.NopCloser(strings.NewReader(oversizedCredentialsJSON())),
				contentLength,
			)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized login status = %d", rec.Code)
			}
		})
	}
	if rec := doRequest(
		login,
		http.MethodPost,
		PathLogin,
		`{"username":"admin","password":"pw"} true`,
	); rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing login status = %d", rec.Code)
	}
}

func TestAuthFormsRejectOversizedBodies(t *testing.T) {
	for name, contentLength := range map[string]int64{
		"declared": int64(len(oversizedCredentialsForm())),
		"streamed": -1,
	} {
		t.Run(name, func(t *testing.T) {
			rec := serveAuthBody(
				htmlSurface(t, testService(t)),
				PathSetupPage,
				formContentType,
				io.NopCloser(strings.NewReader(oversizedCredentialsForm())),
				contentLength,
			)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized form status = %d", rec.Code)
			}
		})
	}
}

func TestAuthFormsRejectMalformedEncoding(t *testing.T) {
	rec := serveAuthBody(
		htmlSurface(t, testService(t)),
		PathSetupPage,
		formContentType,
		io.NopCloser(strings.NewReader("username=%zz&password=password")),
		-1,
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed form status = %d", rec.Code)
	}
}

func TestLoginFormRejectsInvalidAndOversizedCredentials(t *testing.T) {
	service, engine := scriptedService(t)
	injectRawAdmin(t, engine, "operator", dummyPasswordHash)
	oversized := serveAuthBody(
		htmlSurface(t, service),
		PathLoginPage,
		formContentType,
		io.NopCloser(strings.NewReader(oversizedCredentialsForm())),
		-1,
	)
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized login form = %d", oversized.Code)
	}
	invalid := postForm(htmlSurface(t, service), PathLoginPage, url.Values{
		usernameField: {strings.Repeat("u", maximumAdminUsernameBytes+1)},
		passwordField: {"password"},
	})
	if invalid.Header().Get("Location") != PathLoginPage+"?error=invalid" {
		t.Fatalf("invalid login form = %q", invalid.Header().Get("Location"))
	}
}

func TestAuthIntakeBoundsCredentials(t *testing.T) {
	exact := credentialsRequest{
		Username: strings.Repeat("u", maximumAdminUsernameBytes),
		Password: strings.Repeat("p", maximumAdminPasswordBytes),
	}
	if err := validateCredentials(exact); err != nil {
		t.Fatalf("exact credentials rejected: %v", err)
	}
	for name, credentials := range map[string]credentialsRequest{
		"username": {
			Username: exact.Username + "u",
			Password: exact.Password,
		},
		"password": {
			Username: exact.Username,
			Password: exact.Password + "p",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateCredentials(credentials); !errors.Is(err, errCredentialsTooLong) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
	if err := validateCredentials(credentialsRequest{}); !errors.Is(err, errCredentialsRequired) {
		t.Fatalf("missing validation error = %v", err)
	}
	jsonBody := `{"username":"` +
		strings.Repeat("u", maximumAdminUsernameBytes+1) +
		`","password":"password"}`
	if rec := doRequest(
		mountAuth(t, testService(t)),
		http.MethodPost,
		PathSetup,
		jsonBody,
	); rec.Code != http.StatusBadRequest {
		t.Fatalf("long setup JSON = %d", rec.Code)
	}

	service := testService(t)
	rec := postForm(htmlSurface(t, service), PathSetupPage, url.Values{
		usernameField: {strings.Repeat("u", maximumAdminUsernameBytes+1)},
		passwordField: {"password"},
	})
	if rec.Code != http.StatusSeeOther ||
		rec.Header().Get("Location") != PathSetupPage+"?error=invalid" {
		t.Fatalf("long setup form = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func assertAuthBodyUnread(
	t *testing.T,
	handler http.Handler,
	path string,
	wantStatus int,
	wantLocation string,
) {
	t.Helper()
	probe := &authBodyProbe{}
	contentType := formContentType
	if path == PathLogin || path == PathSetup {
		contentType = authJSONMediaType
	}
	rec := serveAuthBody(handler, path, contentType, probe, -1)
	if probe.reads.Load() != 0 {
		t.Fatalf("body reads = %d", probe.reads.Load())
	}
	if rec.Code != wantStatus || rec.Header().Get("Location") != wantLocation {
		t.Fatalf("response = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAuthLoginLimiterPrecedesBody(t *testing.T) {
	limited := testService(t)
	for range limited.limiter.max {
		limited.limiter.recordFailure("192.0.2.1")
	}
	assertAuthBodyUnread(t, mountAuth(t, limited), PathLogin, http.StatusTooManyRequests, "")
	assertAuthBodyUnread(
		t,
		htmlSurface(t, limited),
		PathLoginPage,
		http.StatusSeeOther,
		PathLoginPage+"?error=throttled",
	)
}

func TestAuthSetupStatePrecedesBody(t *testing.T) {
	configured, engine := scriptedService(t)
	injectAdmin(t, engine, "admin", "password")
	assertAuthBodyUnread(t, mountAuth(t, configured), PathSetup, http.StatusConflict, "")
	assertAuthBodyUnread(
		t,
		htmlSurface(t, configured),
		PathSetupPage,
		http.StatusSeeOther,
		PathLoginPage,
	)
	failed, failedEngine := scriptedService(t)
	failedEngine.buckets[adminCredentialsBucket][string(adminKey)] = []byte("{")
	assertAuthBodyUnread(
		t,
		htmlSurface(t, failed),
		PathSetupPage,
		http.StatusSeeOther,
		PathSetupPage+"?error=server",
	)
}

func TestSetupSurfacesPostPrecheckStoreFailure(t *testing.T) {
	useFastCredentialWork(t)
	failed, failedEngine := scriptedService(t)
	failedEngine.putErr = errors.New("write failed")
	if rec := doRequest(
		mountAuth(t, failed),
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"password"}`,
	); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed setup status = %d", rec.Code)
	}
}

func setupWithConcurrentAdmin(t *testing.T) *Service {
	t.Helper()
	useFastCredentialWork(t)
	service := testService(t)
	credentialPasswordHash = func(password string) (string, error) {
		err := service.creds.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return service.creds.records.Put(tx, adminKey, adminRecord{
				Username: "winner", PasswordHash: "hash:winner",
			})
		})
		if err != nil {
			return "", fmt.Errorf("seed concurrent admin: %w", err)
		}

		return "hash:" + password, nil
	}

	return service
}

func TestSetupJSONDetectsPostPrecheckAdministrator(t *testing.T) {
	service := setupWithConcurrentAdmin(t)
	rec := doRequest(
		mountAuth(t, service),
		http.MethodPost,
		PathSetup,
		`{"username":"loser","password":"password"}`,
	)
	if rec.Code != http.StatusConflict {
		t.Fatalf("JSON conflict = %d", rec.Code)
	}
}

func TestSetupHTMLDetectsPostPrecheckAdministrator(t *testing.T) {
	service := setupWithConcurrentAdmin(t)
	rec := postForm(htmlSurface(t, service), PathSetupPage, url.Values{
		usernameField: {"loser"}, passwordField: {"password"},
	})
	if rec.Header().Get("Location") != PathLoginPage {
		t.Fatalf("HTML conflict = %q", rec.Header().Get("Location"))
	}
}

func TestSetupPageShowsCredentialLengthError(t *testing.T) {
	rec := doRequest(
		htmlSurface(t, testService(t)),
		http.MethodGet,
		PathSetupPage+"?error=invalid",
		"",
	)
	if !strings.Contains(rec.Body.String(), "Username or password is too long.") {
		t.Fatalf("setup error page = %s", rec.Body.String())
	}
}
