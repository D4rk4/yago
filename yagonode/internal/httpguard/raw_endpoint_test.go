package httpguard_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type unreadRawBody struct {
	read bool
}

func (body *unreadRawBody) Read([]byte) (int, error) {
	body.read = true

	return 0, io.EOF
}

func (*unreadRawBody) Close() error { return nil }

func rawGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(64, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func TestServeRawWritesPlainResponse(t *testing.T) {
	handler := httpguard.ServeRaw(
		rawGate(),
		yagoproto.EndpointMethodsGetPost,
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
		yagoproto.PathSeedlist+"?value=%3Cok%3E%26x%3D%7B%7B.%7D%7D",
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
		yagoproto.EndpointMethodsGetPost,
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
		yagoproto.PathSeedlist,
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
		yagoproto.EndpointMethodsGetPost,
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
		yagoproto.PathSeedlist,
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
		yagoproto.EndpointMethodsGetPost,
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
		yagoproto.PathSeedlist,
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
		yagoproto.PathSeedlist,
		yagoproto.EndpointMethodsGetPost,
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
		yagoproto.PathSeedlist+"?value=ok",
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

func TestServeRawWithAdmissionShedsAndReleases(t *testing.T) {
	admission := httpguard.NewIntakeGate(1)
	release, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("failed to reserve admission fixture")
	}
	handler := httpguard.ServeRawWithAdmission(
		rawGate(),
		yagoproto.EndpointMethodsGetPost,
		func(context.Context, url.Values) (struct{}, error) {
			return struct{}{}, nil
		},
		func(context.Context, struct{}) (httpguard.RawResponse, error) {
			return httpguard.RawResponse{}, errors.New("boom")
		},
		admission,
	)
	body := &unreadRawBody{}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathSeedlist,
		body,
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("saturated status = %d retry=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if body.read {
		t.Fatal("saturated intake read the request body")
	}
	release()

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("serve error status = %d, want 500", rec.Code)
	}
	finalRelease, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("serve error retained admission slot")
	}
	finalRelease()
}

func TestMountRawWithAdmissionServesMuxRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, rawGate())
	httpguard.MountRawWithAdmission(
		router,
		httpguard.RawRouteAdmission[string]{
			Path:    yagoproto.PathSeedlist,
			Methods: yagoproto.EndpointMethodsGetPost,
			Parse: func(context.Context, url.Values) (string, error) {
				return "ok", nil
			},
			Serve: func(context.Context, string) (httpguard.RawResponse, error) {
				return httpguard.RawResponse{Body: "admitted"}, nil
			},
			Admission: httpguard.NewIntakeGate(1),
		},
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSeedlist,
		nil,
	))
	if rec.Code != http.StatusOK || rec.Body.String() != "admitted" {
		t.Fatalf("mounted response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestServeRawWithAdmissionReleasesRouteWhenWireIntakeSheds(t *testing.T) {
	wireIntake := httpguard.NewIntakeGate(1)
	releaseWire, admitted := wireIntake.TryAcquire()
	if !admitted {
		t.Fatal("failed to reserve wire intake fixture")
	}
	routeIntake := httpguard.NewIntakeGate(1)
	gate := rawGate()
	gate.Intake = wireIntake
	handler := httpguard.ServeRawWithAdmission(
		gate,
		yagoproto.EndpointMethodsGetPost,
		func(context.Context, url.Values) (struct{}, error) { return struct{}{}, nil },
		func(context.Context, struct{}) (httpguard.RawResponse, error) {
			return httpguard.RawResponse{}, nil
		},
		routeIntake,
	)
	body := &unreadRawBody{}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathSeedlist,
		body,
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusServiceUnavailable || body.read {
		t.Fatalf("wire saturation = %d read=%t", rec.Code, body.read)
	}
	releaseRoute, admitted := routeIntake.TryAcquire()
	if !admitted {
		t.Fatal("wire rejection retained route admission")
	}
	releaseRoute()
	releaseWire()
}
