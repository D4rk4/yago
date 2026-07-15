package adminauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func authRequestWithMediaType(
	t *testing.T,
	handler http.Handler,
	path, mediaType, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		path,
		strings.NewReader(body),
	)
	if mediaType != "" {
		req.Header.Set("Content-Type", mediaType)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func TestAuthCredentialEndpointsRequireJSONMediaType(t *testing.T) {
	body := `{"username":"admin","password":"pw"}`
	for _, path := range []string{PathSetup, PathLogin} {
		for _, mediaType := range []string{
			"",
			"text/plain",
			"application/x-www-form-urlencoded",
			"application/json-patch+json",
			"application/json; broken",
		} {
			t.Run(path+"/"+mediaType, func(t *testing.T) {
				service := testService(t)
				rec := authRequestWithMediaType(t, mountAuth(t, service), path, mediaType, body)
				if rec.Code != http.StatusUnsupportedMediaType {
					t.Fatalf("status = %d, want 415", rec.Code)
				}
				if present, err := service.creds.exists(t.Context()); err != nil || present {
					t.Fatalf(
						"rejected request changed credentials: present=%v err=%v",
						present,
						err,
					)
				}
			})
		}
	}
}

func TestCrossSiteTextPlainSetupCannotCreateAdmin(t *testing.T) {
	service := testService(t)
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSetup,
		strings.NewReader(`{"username":"attacker","password":"pw"}`),
	)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "https://hostile.example")
	rec := httptest.NewRecorder()
	mountAuth(t, service).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("hostile setup status = %d, want 415", rec.Code)
	}
	if present, err := service.creds.exists(t.Context()); err != nil || present {
		t.Fatalf("hostile setup changed credentials: present=%v err=%v", present, err)
	}
}

func TestAuthCredentialEndpointsAcceptJSONParameters(t *testing.T) {
	useFastCredentialWork(t)
	service := testService(t)
	handler := mountAuth(t, service)
	body := `{"username":"admin","password":"pw"}`
	setup := authRequestWithMediaType(
		t,
		handler,
		PathSetup,
		"application/json; charset=utf-8",
		body,
	)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, body=%s", setup.Code, setup.Body.String())
	}
	login := authRequestWithMediaType(
		t,
		handler,
		PathLogin,
		"application/json; charset=UTF-8",
		body,
	)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", login.Code, login.Body.String())
	}
}
