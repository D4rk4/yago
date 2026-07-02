package httpguard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

func rawGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(64, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func TestServeRawWritesPlainResponse(t *testing.T) {
	handler := httpguard.ServeRaw(
		rawGate(),
		yacyproto.EndpointMethodsGetPost,
		func(_ context.Context, form url.Values) (string, error) {
			return form.Get("value"), nil
		},
		func(_ context.Context, value string) (httpguard.RawResponse, error) {
			return httpguard.RawResponse{
				ContentType: "text/plain; charset=UTF-8",
				Body:        "value=" + value,
			}, nil
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSeedlist+"?value=%3Cok%3E%26x%3D%7B%7B.%7D%7D",
		nil,
	)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=UTF-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := rec.Body.String(); got != "value=<ok>&x={{.}}" {
		t.Fatalf("body = %q", got)
	}
}

func TestServeRawRejectsParseFailure(t *testing.T) {
	handler := httpguard.ServeRaw(
		rawGate(),
		yacyproto.EndpointMethodsGetPost,
		func(context.Context, url.Values) (struct{}, error) {
			return struct{}{}, errors.New("bad request")
		},
		func(context.Context, struct{}) (httpguard.RawResponse, error) {
			t.Fatal("serve called after parse failure")
			return httpguard.RawResponse{}, nil
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSeedlist,
		nil,
	)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestServeRawRejectsDisallowedMethod(t *testing.T) {
	handler := httpguard.ServeRaw(
		rawGate(),
		yacyproto.EndpointMethodsGetPost,
		func(context.Context, url.Values) (struct{}, error) {
			return struct{}{}, nil
		},
		func(context.Context, struct{}) (httpguard.RawResponse, error) {
			t.Fatal("serve called after method rejection")
			return httpguard.RawResponse{}, nil
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		yacyproto.PathSeedlist,
		nil,
	)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestServeRawMapsServeErrorToInternal(t *testing.T) {
	handler := httpguard.ServeRaw(
		rawGate(),
		yacyproto.EndpointMethodsGetPost,
		func(context.Context, url.Values) (struct{}, error) {
			return struct{}{}, nil
		},
		func(context.Context, struct{}) (httpguard.RawResponse, error) {
			return httpguard.RawResponse{}, errors.New("boom")
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSeedlist,
		nil,
	)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestMountRawServesMuxRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, rawGate())
	httpguard.MountRaw(
		router,
		yacyproto.PathSeedlist,
		yacyproto.EndpointMethodsGetPost,
		func(_ context.Context, form url.Values) (string, error) {
			return form.Get("value"), nil
		},
		func(_ context.Context, value string) (httpguard.RawResponse, error) {
			return httpguard.RawResponse{Body: "mounted=" + value}, nil
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSeedlist+"?value=ok",
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "mounted=ok" {
		t.Fatalf("body = %q", got)
	}
}
