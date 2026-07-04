package crossorigin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/crossorigin"
)

func nextHandler() (http.Handler, *bool) {
	served := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})

	return handler, &served
}

func request(method, origin, requestMethod string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), method, "/api", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if requestMethod != "" {
		req.Header.Set("Access-Control-Request-Method", requestMethod)
	}

	return req
}

func adminPolicy(origins ...string) *crossorigin.Policy {
	return crossorigin.NewPolicy(crossorigin.Config{
		AllowedOrigins:   origins,
		AllowCredentials: true,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost},
		AllowedHeaders:   []string{"Content-Type", "X-CSRF-Token"},
		MaxAge:           10 * time.Minute,
	})
}

func TestWrapPassesRequestWithoutOrigin(t *testing.T) {
	handler, served := nextHandler()
	rec := httptest.NewRecorder()
	adminPolicy("https://ui.example").Wrap(handler).ServeHTTP(rec, request(http.MethodGet, "", ""))
	if !*served {
		t.Fatal("handler should serve same-origin requests")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS header expected without an Origin")
	}
}

func TestWrapAllowsListedOrigin(t *testing.T) {
	handler, served := nextHandler()
	rec := httptest.NewRecorder()
	adminPolicy("https://ui.example").Wrap(handler).
		ServeHTTP(rec, request(http.MethodGet, "https://ui.example", ""))
	if !*served {
		t.Fatal("allowed origin should reach the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://ui.example" {
		t.Fatalf("allow-origin = %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentialed policy should allow credentials")
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatal("Vary: Origin expected")
	}
}

func TestWrapDeniesUnlistedOrigin(t *testing.T) {
	handler, served := nextHandler()
	rec := httptest.NewRecorder()
	adminPolicy("https://ui.example").Wrap(handler).
		ServeHTTP(rec, request(http.MethodGet, "https://evil.example", ""))
	if !*served {
		t.Fatal("denied origin still reaches the handler; the browser blocks the read")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no allow-origin header for a denied origin")
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatal("Vary: Origin expected even when denied")
	}
}

func TestWrapAnswersAllowedPreflight(t *testing.T) {
	handler, served := nextHandler()
	rec := httptest.NewRecorder()
	adminPolicy("https://ui.example").Wrap(handler).
		ServeHTTP(rec, request(http.MethodOptions, "https://ui.example", http.MethodPost))
	if *served {
		t.Fatal("preflight must not reach the handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" ||
		rec.Header().Get("Access-Control-Allow-Headers") == "" ||
		rec.Header().Get("Access-Control-Max-Age") != "600" {
		t.Fatalf("preflight headers = %#v", rec.Header())
	}
}

func TestWrapAnswersDeniedPreflightWithoutHeaders(t *testing.T) {
	handler, served := nextHandler()
	rec := httptest.NewRecorder()
	adminPolicy("https://ui.example").Wrap(handler).
		ServeHTTP(rec, request(http.MethodOptions, "https://evil.example", http.MethodPost))
	if *served {
		t.Fatal("preflight must not reach the handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Fatal("denied preflight must not advertise CORS headers")
	}
}

func TestWrapWildcardWithoutCredentialsUsesStar(t *testing.T) {
	handler, _ := nextHandler()
	rec := httptest.NewRecorder()
	policy := crossorigin.NewPolicy(crossorigin.Config{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{http.MethodGet},
	})
	policy.Wrap(handler).ServeHTTP(rec, request(http.MethodGet, "https://any.example", ""))
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q, want *", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("non-credentialed policy must not allow credentials")
	}
}

func TestWrapWildcardWithCredentialsEchoesOrigin(t *testing.T) {
	handler, _ := nextHandler()
	rec := httptest.NewRecorder()
	adminPolicy(
		"*",
	).Wrap(handler).
		ServeHTTP(rec, request(http.MethodGet, "https://any.example", ""))
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://any.example" {
		t.Fatalf("allow-origin = %q, want the echoed origin", got)
	}
}
