package adminauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func reachedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func apiKeyGuarded(t *testing.T, service *Service) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	Mount(mux, service)
	mux.Handle("/protected", reachedHandler())
	mux.Handle("/crawl", reachedHandler())

	return service.Guard(
		[]string{PathLogin, PathSetup},
		map[string]Scope{"/crawl": ScopeCrawlWrite},
		mux,
	)
}

func TestBearerTokenVariants(t *testing.T) {
	cases := []struct {
		header  string
		wantOK  bool
		wantTok string
	}{
		{"Bearer abc", true, "abc"},
		{"bearer abc", true, "abc"},
		{"", false, ""},
		{"Bearer", false, ""},
		{"Basic abc", false, ""},
		{"Bearer   ", false, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		if c.header != "" {
			req.Header.Set(authzHeader, c.header)
		}
		tok, ok := bearerToken(req)
		if ok != c.wantOK || tok != c.wantTok {
			t.Fatalf(
				"bearerToken(%q) = %q, %v; want %q, %v",
				c.header,
				tok,
				ok,
				c.wantTok,
				c.wantOK,
			)
		}
	}
}

func TestGuardAPIKeyAllowsWithMatchingScope(t *testing.T) {
	service := testService(t)
	surface := apiKeyGuarded(t, service)
	key := createKey(t, service, ScopeAdminRead)
	rec := doBearerRequest(surface, http.MethodGet, "/protected", key.Key)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGuardAPIKeyRejectsInsufficientScope(t *testing.T) {
	service := testService(t)
	surface := apiKeyGuarded(t, service)
	key := createKey(t, service, ScopeSearchRead)
	rec := doBearerRequest(surface, http.MethodGet, "/protected", key.Key)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestGuardAPIKeyUnsafeMethodDefaultsToAdminWrite(t *testing.T) {
	service := testService(t)
	surface := apiKeyGuarded(t, service)
	readKey := createKey(t, service, ScopeAdminRead)
	if rec := doBearerRequest(
		surface,
		http.MethodPost,
		"/protected",
		readKey.Key,
	); rec.Code != http.StatusForbidden {
		t.Fatalf("admin:read POST = %d, want 403", rec.Code)
	}
	writeKey := createKey(t, service, ScopeAdminWrite)
	if rec := doBearerRequest(
		surface,
		http.MethodPost,
		"/protected",
		writeKey.Key,
	); rec.Code != http.StatusOK {
		t.Fatalf("admin:write POST = %d, want 200", rec.Code)
	}
}

func TestGuardAPIKeyHonorsCrawlScopeOverride(t *testing.T) {
	service := testService(t)
	surface := apiKeyGuarded(t, service)
	writeKey := createKey(t, service, ScopeAdminWrite)
	if rec := doBearerRequest(
		surface,
		http.MethodPost,
		"/crawl",
		writeKey.Key,
	); rec.Code != http.StatusForbidden {
		t.Fatalf("admin:write on /crawl = %d, want 403", rec.Code)
	}
	crawlKey := createKey(t, service, ScopeCrawlWrite)
	if rec := doBearerRequest(
		surface,
		http.MethodPost,
		"/crawl",
		crawlKey.Key,
	); rec.Code != http.StatusOK {
		t.Fatalf("crawl:write on /crawl = %d, want 200", rec.Code)
	}
}

func TestGuardAPIKeyRejectsMalformedBearer(t *testing.T) {
	surface := apiKeyGuarded(t, testService(t))
	rec := doBearerRequest(surface, http.MethodGet, "/protected", "not-a-key")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGuardAPIKeyRejectsUnknownKey(t *testing.T) {
	surface := apiKeyGuarded(t, testService(t))
	// A well-formed key minted by an unrelated store: it parses but its
	// identifier is absent from the guarded service.
	foreign := createKey(t, testService(t), ScopeAdminRead)
	rec := doBearerRequest(surface, http.MethodGet, "/protected", foreign.Key)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGuardAPIKeyRateLimited(t *testing.T) {
	service, err := New(testVault(t), Config{APIKeyMaxPerWindow: 1, APIKeyWindow: time.Minute})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	surface := apiKeyGuarded(t, service)
	key := createKey(t, service, ScopeAdminRead)
	if rec := doBearerRequest(
		surface,
		http.MethodGet,
		"/protected",
		key.Key,
	); rec.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", rec.Code)
	}
	if rec := doBearerRequest(
		surface,
		http.MethodGet,
		"/protected",
		key.Key,
	); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", rec.Code)
	}
}

func TestGuardAPIKeySurfacesAuthError(t *testing.T) {
	service, engine := scriptedService(t)
	surface := apiKeyGuarded(t, service)
	key := createKey(t, service, ScopeAdminRead)
	engine.buckets[adminAPIKeysBucket][key.ID] = []byte("{corrupt")
	rec := doBearerRequest(surface, http.MethodGet, "/protected", key.Key)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestGuardBearerTakesPrecedenceOverCookie(t *testing.T) {
	service := testService(t)
	surface := apiKeyGuarded(t, service)
	cookie, _ := loginThroughGuard(t, surface)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.AddCookie(cookie)
	req.Header.Set(authzHeader, bearerScheme+"not-a-key")
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (bearer path used despite valid cookie)", rec.Code)
	}
}
