package adminauth

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrustedProxyRequestPolicyAcceptsOnlyTrustedForwarding(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("10.20.0.0/16")
	policy := trustedProxyRequestPolicy{trustedProxies: []*net.IPNet{trusted}}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://node.example/admin/",
		nil,
	)
	request.RemoteAddr = "10.20.0.5:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.8, 10.20.0.4")
	request.Header.Set("X-Forwarded-Proto", "https")
	if got := policy.ClientAddress(request); got != "198.51.100.8" {
		t.Fatalf("trusted forwarded client = %q, want 198.51.100.8", got)
	}
	if !policy.Secure(request) {
		t.Fatal("trusted HTTPS proxy was not treated as secure")
	}

	request.RemoteAddr = "192.0.2.10:443"
	request.TLS = nil
	if got := policy.ClientAddress(request); got != "192.0.2.10" {
		t.Fatalf("untrusted forwarded client = %q, want 192.0.2.10", got)
	}
	if policy.Secure(request) {
		t.Fatal("untrusted forwarded HTTPS header made the request secure")
	}
	request.TLS = &tls.ConnectionState{}
	if !policy.Secure(request) {
		t.Fatal("direct TLS request was not treated as secure")
	}
}

func TestTrustedProxyClientAddressRejectsInvalidAndUntrustedForwarding(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("10.20.0.0/16")
	policy := trustedProxyRequestPolicy{trustedProxies: []*net.IPNet{trusted}}
	cases := []struct {
		name        string
		remote      string
		forwarded   string
		wantAddress string
	}{
		{
			name:        "invalid remote address",
			remote:      "unparsed-remote",
			forwarded:   "198.51.100.9",
			wantAddress: "unparsed-remote",
		},
		{
			name:        "missing forwarded address",
			remote:      "10.20.0.5:443",
			wantAddress: "10.20.0.5",
		},
		{
			name:        "malformed forwarded address",
			remote:      "10.20.0.5:443",
			forwarded:   "198.51.100.9, invalid-address",
			wantAddress: "10.20.0.5",
		},
		{
			name:        "untrusted forwarding hop",
			remote:      "10.20.0.5:443",
			forwarded:   "198.51.100.9, 203.0.113.2",
			wantAddress: "203.0.113.2",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				"http://node.example/admin/",
				nil,
			)
			request.RemoteAddr = testCase.remote
			request.Header.Set("X-Forwarded-For", testCase.forwarded)
			if got := policy.ClientAddress(request); got != testCase.wantAddress {
				t.Fatalf("client address = %q, want %q", got, testCase.wantAddress)
			}
		})
	}
}

func TestTrustedProxySetsSetupCookieFromForwardedScheme(t *testing.T) {
	handler := trustedProxyAuthServer(t, 3)
	securePage := proxyRequest(t, handler, proxyRequestDetails{
		method: http.MethodGet, path: PathSetupPage,
		forwardedClient: "203.0.113.1", forwardedScheme: "https",
	})
	setupCookie := responseCookie(securePage, setupFormCookieName)
	if setupCookie == nil || !setupCookie.Secure {
		t.Fatalf("trusted HTTPS setup cookie = %+v, want Secure setup cookie", setupCookie)
	}

	plainPage := proxyRequest(t, handler, proxyRequestDetails{
		method: http.MethodGet, path: PathSetupPage,
		forwardedClient: "203.0.113.1", forwardedScheme: "http",
	})
	plainCookie := responseCookie(plainPage, setupFormCookieName)
	if plainCookie == nil || plainCookie.Secure {
		t.Fatalf("trusted HTTP setup cookie = %+v, want non-Secure setup cookie", plainCookie)
	}
}

func TestTrustedProxyRateLimitsByForwardedClient(t *testing.T) {
	handler := trustedProxyAuthServer(t, 1)
	setupProxyTestAdmin(t, handler)
	badLogin := `{"username":"admin","password":"wrong"}`
	firstAttempt := proxyRequest(t, handler, proxyLogin("198.51.100.5", badLogin))
	if firstAttempt.Code != http.StatusUnauthorized {
		t.Fatalf("first caller attempt = %d, want 401", firstAttempt.Code)
	}
	sameCallerAttempt := proxyRequest(t, handler, proxyLogin("198.51.100.5", badLogin))
	if sameCallerAttempt.Code != http.StatusTooManyRequests {
		t.Fatalf("same caller attempt = %d, want 429", sameCallerAttempt.Code)
	}
	differentCallerAttempt := proxyRequest(t, handler, proxyLogin("198.51.100.6", badLogin))
	if differentCallerAttempt.Code != http.StatusUnauthorized {
		t.Fatalf("different caller attempt = %d, want 401", differentCallerAttempt.Code)
	}
}

func TestTrustedProxyLoginSetsSecureSessionCookie(t *testing.T) {
	handler := trustedProxyAuthServer(t, 3)
	setupProxyTestAdmin(t, handler)
	validLogin := `{"username":"admin","password":"pw"}`
	login := proxyRequest(t, handler, proxyLogin("198.51.100.5", validLogin))
	if login.Code != http.StatusOK {
		t.Fatalf("trusted proxy login = %d, want 200", login.Code)
	}
	sessionCookie := responseCookie(login, sessionCookieName)
	if sessionCookie == nil || !sessionCookie.Secure {
		t.Fatalf("trusted HTTPS session cookie = %+v, want Secure session cookie", sessionCookie)
	}
}

type proxyRequestDetails struct {
	method          string
	path            string
	forwardedClient string
	forwardedScheme string
	body            string
}

func proxyRequest(
	t *testing.T,
	handler http.Handler,
	details proxyRequestDetails,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		details.method,
		"http://node.example"+details.path,
		strings.NewReader(details.body),
	)
	request.RemoteAddr = "10.30.0.2:443"
	request.Header.Set("X-Forwarded-For", details.forwardedClient)
	request.Header.Set("X-Forwarded-Proto", details.forwardedScheme)
	if details.method == http.MethodPost && details.path == PathLogin {
		request.Header.Set("Content-Type", authJSONMediaType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

func trustedProxyAuthServer(t *testing.T, attempts int) *http.ServeMux {
	t.Helper()
	service := testService(t)
	_, trusted, _ := net.ParseCIDR("10.30.0.0/16")
	service.requestTrust = trustedProxyRequestPolicy{trustedProxies: []*net.IPNet{trusted}}
	service.limiter = newLoginRateLimiter(attempts, time.Minute, time.Now)
	handler := http.NewServeMux()
	Mount(handler, service)
	MountHTML(handler, service)

	return handler
}

func setupProxyTestAdmin(t *testing.T, handler http.Handler) {
	t.Helper()
	response := doRequest(
		handler,
		http.MethodPost,
		PathSetup,
		`{"username":"admin","password":"pw"}`,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("setup admin status = %d, want 201", response.Code)
	}
}

func proxyLogin(forwardedClient, body string) proxyRequestDetails {
	return proxyRequestDetails{
		method: http.MethodPost, path: PathLogin,
		forwardedClient: forwardedClient, forwardedScheme: "https", body: body,
	}
}

func responseCookie(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}
