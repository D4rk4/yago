package httpguard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type echoResponse struct {
	addr string
}

func (r echoResponse) Encode() yagomodel.Message {
	return yagomodel.Message{"yourip": r.addr}
}

func testGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   testGuard(),
		Respond: httpguard.NewWireResponder(stubStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func postForm() *http.Request {
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yagoproto.PathTransferURL,
		strings.NewReader("a=b"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req
}

func TestServeWritesResponseWithRemoteAddr(t *testing.T) {
	handler := httpguard.Serve(
		testGate(),
		yagoproto.TransferURLEndpointMethods,
		func(ctx context.Context, _ url.Values) (echoResponse, error) {
			return echoResponse{addr: httpguard.RemoteAddr(ctx)}, nil
		},
		func(_ context.Context, resp echoResponse) (echoResponse, error) {
			return resp, nil
		},
	)

	rec := httptest.NewRecorder()
	req := postForm()
	req.RemoteAddr = "203.0.113.9:5000"
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "203.0.113.9") {
		t.Fatalf("body = %q, want resolved remote address", rec.Body.String())
	}
}

func TestServeMapsParseErrorToBadRequest(t *testing.T) {
	handler := httpguard.Serve(
		testGate(),
		yagoproto.TransferURLEndpointMethods,
		func(context.Context, url.Values) (echoResponse, error) {
			return echoResponse{}, errors.New("bad form")
		},
		func(_ context.Context, resp echoResponse) (echoResponse, error) {
			return resp, nil
		},
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, postForm())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestServeMapsServeErrorToInternal(t *testing.T) {
	handler := httpguard.Serve(
		testGate(),
		yagoproto.TransferURLEndpointMethods,
		func(context.Context, url.Values) (echoResponse, error) {
			return echoResponse{}, nil
		},
		func(context.Context, echoResponse) (echoResponse, error) {
			return echoResponse{}, errors.New("boom")
		},
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, postForm())

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestServeRejectsDisallowedMethod(t *testing.T) {
	handler := httpguard.Serve(
		testGate(),
		yagoproto.TransferURLEndpointMethods,
		func(context.Context, url.Values) (echoResponse, error) {
			return echoResponse{}, nil
		},
		func(_ context.Context, resp echoResponse) (echoResponse, error) {
			return resp, nil
		},
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yagoproto.PathTransferURL,
		nil,
	))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestServeWireIntakeShedsBeforeBodyAndReleases(t *testing.T) {
	intake := httpguard.NewIntakeGate(1)
	release, admitted := intake.TryAcquire()
	if !admitted {
		t.Fatal("failed to reserve wire intake fixture")
	}
	gate := testGate()
	gate.Intake = intake
	handler := httpguard.Serve(
		gate,
		yagoproto.TransferURLEndpointMethods,
		func(context.Context, url.Values) (echoResponse, error) {
			return echoResponse{}, nil
		},
		func(_ context.Context, resp echoResponse) (echoResponse, error) {
			return resp, nil
		},
	)
	body := &unreadRawBody{}
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathTransferURL,
		body,
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" ||
		body.read {
		t.Fatalf(
			"saturated response = %d retry=%q read=%t",
			rec.Code,
			rec.Header().Get("Retry-After"),
			body.read,
		)
	}
	release()

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, postForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("released response = %d, want 200", rec.Code)
	}
	finalRelease, admitted := intake.TryAcquire()
	if !admitted {
		t.Fatal("successful wire response retained intake slot")
	}
	finalRelease()
}

func TestMountServesMuxRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, testGate())
	httpguard.Mount(
		router,
		yagoproto.PathTransferURL,
		yagoproto.TransferURLEndpointMethods,
		func(ctx context.Context, _ url.Values) (echoResponse, error) {
			return echoResponse{addr: httpguard.RemoteAddr(ctx)}, nil
		},
		func(_ context.Context, resp echoResponse) (echoResponse, error) {
			return resp, nil
		},
	)

	rec := httptest.NewRecorder()
	req := postForm()
	req.RemoteAddr = "203.0.113.10:5000"
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "203.0.113.10") {
		t.Fatalf("body = %q, want mounted response", rec.Body.String())
	}
}
