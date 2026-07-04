package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func passThrough(hit *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*hit = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestRedirectHTTPSDisabledPassesThrough(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	hit := false
	handler := redirectHTTPS(toggles, passThrough(&hit))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(
		rec,
		httptest.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"http://example.com/x?q=1",
			nil,
		),
	)
	if !hit || rec.Code != http.StatusOK {
		t.Fatalf("expected pass-through, got code %d hit=%v", rec.Code, hit)
	}
}

func TestRedirectHTTPSRedirectsPlainHTTP(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	toggles.SetHTTPSRedirect(true)
	hit := false
	handler := redirectHTTPS(toggles, passThrough(&hit))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(
		rec,
		httptest.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"http://example.com/search?q=cats",
			nil,
		),
	)
	if hit {
		t.Fatal("inner handler was reached despite redirect")
	}
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("code = %d, want 308", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://example.com/search?q=cats" {
		t.Fatalf("Location = %q, want the https origin with path and query", got)
	}
}

func TestRedirectHTTPSHonoursForwardedProto(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	toggles.SetHTTPSRedirect(true)
	hit := false
	handler := redirectHTTPS(toggles, passThrough(&hit))

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://example.com/x",
		nil,
	)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !hit {
		t.Fatal("request already on https was redirected")
	}
}

func TestRedirectHTTPSSkipsLoopback(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	toggles.SetHTTPSRedirect(true)
	hit := false
	handler := redirectHTTPS(toggles, passThrough(&hit))

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://127.0.0.1:9090/admin/overview",
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !hit {
		t.Fatal("loopback admin request was redirected; lock-out guardrail failed")
	}
}

func TestRootDispatcherServesPortalWhenEnabled(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	toggles.SetPortalEnabled(true)
	portalHit := false
	dispatcher := &rootDispatcher{
		toggles: toggles,
		portal:  passThrough(&portalHit),
		landing: http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) { t.Error("landing served") },
		),
	}

	dispatcher.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil),
	)
	if !portalHit {
		t.Fatal("portal not served while enabled")
	}
}

func TestRootDispatcherServesLandingWhenDisabled(t *testing.T) {
	t.Parallel()

	toggles := &runtimeToggles{}
	landingHit := false
	dispatcher := &rootDispatcher{
		toggles: toggles,
		portal: http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) { t.Error("portal served") },
		),
		landing: passThrough(&landingHit),
	}

	dispatcher.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil),
	)
	if !landingHit {
		t.Fatal("landing not served while portal disabled")
	}
}
