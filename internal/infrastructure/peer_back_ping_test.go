package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func backPingPeer(t *testing.T, server *httptest.Server) yacymodel.Seed {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host, err := yacymodel.ParseHost(parsed.Hostname())
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}
	port, err := yacymodel.ParsePort(parsed.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	return yacymodel.Seed{
		Hash: hashForTest(t),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(port),
	}
}

func hashForTest(t *testing.T) yacymodel.Hash {
	t.Helper()

	h, err := yacymodel.ParseHash("AAAAAAAAAAAA")
	if err != nil {
		t.Fatalf("parse hash: %v", err)
	}

	return h
}

func TestPeerBackPingReachable(t *testing.T) {
	var gotObject string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotObject = r.URL.Query().Get(yacyproto.FieldObject)
		resp := yacyproto.QueryResponse{Response: 0}
		_, _ = w.Write([]byte(resp.Encode().Encode()))
	}))
	defer server.Close()

	pinger := NewPeerBackPing(server.Client(), hashForTest(t), "freeworld")
	if err := pinger.Ping(context.Background(), backPingPeer(t, server)); err != nil {
		t.Fatalf("expected reachable, got %v", err)
	}
	if gotObject != string(yacyproto.ObjectRWICount) {
		t.Errorf("object = %q, want %q", gotObject, yacyproto.ObjectRWICount)
	}
}

func TestPeerBackPingNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	pinger := NewPeerBackPing(server.Client(), hashForTest(t), "freeworld")
	if err := pinger.Ping(context.Background(), backPingPeer(t, server)); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestPeerBackPingNoAddress(t *testing.T) {
	pinger := NewPeerBackPing(http.DefaultClient, hashForTest(t), "freeworld")
	peer := yacymodel.Seed{Hash: hashForTest(t)}
	if err := pinger.Ping(context.Background(), peer); err == nil {
		t.Fatal("expected error when peer has no address")
	}
}
