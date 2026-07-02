package httpguard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type echoResponse struct {
	addr string
}

func (r echoResponse) Encode() yacymodel.Message {
	return yacymodel.Message{"yourip": r.addr}
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
		yacyproto.PathTransferURL,
		strings.NewReader("a=b"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req
}

func TestServeWritesResponseWithRemoteAddr(t *testing.T) {
	handler := httpguard.Serve(
		testGate(),
		yacyproto.TransferURLEndpointMethods,
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
		yacyproto.TransferURLEndpointMethods,
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
		yacyproto.TransferURLEndpointMethods,
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
		yacyproto.TransferURLEndpointMethods,
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
		yacyproto.PathTransferURL,
		nil,
	))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestMountServesMuxRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, testGate())
	httpguard.Mount(
		router,
		yacyproto.PathTransferURL,
		yacyproto.TransferURLEndpointMethods,
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
