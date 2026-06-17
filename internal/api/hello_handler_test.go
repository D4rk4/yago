package api

import (
	"net"
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestHelloHandlerHappyPath(t *testing.T) {
	h := newTestHarness(t)
	known := testSeed(t, "friend", "friend")
	h.peers.outcome = contracts.HelloOutcome{
		CallerType: yacymodel.PeerSenior,
		Known:      []yacymodel.Seed{known},
	}

	req := yacyproto.HelloRequest{Seed: testSeed(t, "caller", "caller"), Iam: testHash(t, "caller")}
	rec := h.do(t, http.MethodPost, yacyproto.PathHello, req.Form())

	resp, err := yacyproto.ParseHelloResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if !h.peers.called {
		t.Fatal("peers.Hello was not called")
	}
	if resp.YourType != yacymodel.PeerSenior {
		t.Errorf("YourType = %q, want senior", resp.YourType)
	}
	if len(resp.Seeds) != 2 {
		t.Fatalf("seeds = %d, want 2", len(resp.Seeds))
	}
	if resp.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", resp.Version)
	}
}

func TestHelloHandlerWrongMethod(t *testing.T) {
	h := newTestHarness(t)
	rec := h.do(t, http.MethodDelete, yacyproto.PathHello, nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHelloHandlerWrongNetwork(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.HelloRequest{NetworkName: "othernet", Seed: testSeed(t, "caller", "caller")}
	rec := h.do(t, http.MethodPost, yacyproto.PathHello, req.Form())

	decodeResponse(t, rec)
	if h.peers.called {
		t.Fatal("peers.Hello must not be called on network mismatch")
	}
}

func TestHelloHandlerForwardedFor(t *testing.T) {
	h := newTestHarness(t)
	_, trusted, _ := net.ParseCIDR("192.0.2.0/24")

	req := yacyproto.HelloRequest{Seed: testSeed(t, "caller", "caller")}
	httpReq := httptestForwarded(
		t,
		req.Form(),
		yacyproto.PathHello,
		"192.0.2.10:1234",
		"203.0.113.5",
	)

	rec := newRecorder()
	h.mux(WithTrustedProxies([]*net.IPNet{trusted})).ServeHTTP(rec, httpReq)

	resp, err := yacyproto.ParseHelloResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.YourIP != "203.0.113.5" {
		t.Errorf("YourIP = %q, want forwarded address", resp.YourIP)
	}
}
