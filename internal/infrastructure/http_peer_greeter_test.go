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

func selfSeed(t *testing.T) yacymodel.Seed {
	t.Helper()

	return yacymodel.Seed{
		yacymodel.SeedHash: string(hashForTest(t)),
		yacymodel.SeedName: "self",
		yacymodel.SeedIP:   "203.0.113.9",
		yacymodel.SeedPort: "8090",
	}
}

func endpointOf(t *testing.T, server *httptest.Server) string {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	return parsed.Host
}

func TestPeerGreeterLearnsTypeAndKnownSeeds(t *testing.T) {
	var gotIam, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotIam = r.PostForm.Get(yacyproto.FieldIam)

		known := yacymodel.Seed{
			yacymodel.SeedHash: string(hashForTest(t)),
			yacymodel.SeedIP:   "198.51.100.7",
			yacymodel.SeedPort: "8090",
		}
		resp := yacyproto.HelloResponse{
			YourIP:   "203.0.113.9",
			YourType: yacymodel.PeerSenior,
			Seeds:    []yacymodel.Seed{selfSeed(t), known},
		}
		_, _ = w.Write([]byte(resp.Encode().Encode()))
	}))
	defer server.Close()

	greeter := NewHTTPPeerGreeter(server.Client(), "freeworld")
	result, err := greeter.Greet(context.Background(), endpointOf(t, server), selfSeed(t), 0)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotPath != yacyproto.PathHello {
		t.Errorf("path = %q, want %q", gotPath, yacyproto.PathHello)
	}
	if gotIam != string(hashForTest(t)) {
		t.Errorf("iam = %q, want self hash", gotIam)
	}
	if result.YourType != yacymodel.PeerSenior {
		t.Errorf("type = %v, want senior", result.YourType)
	}
	if result.YourIP != "203.0.113.9" {
		t.Errorf("yourip = %q, want advertised ip", result.YourIP)
	}
	if len(result.Known) != 1 {
		t.Fatalf("known = %d, want 1 (own seed excluded)", len(result.Known))
	}
}

func TestPeerGreeterRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	greeter := NewHTTPPeerGreeter(server.Client(), "freeworld")
	if _, err := greeter.Greet(
		context.Background(),
		endpointOf(t, server),
		selfSeed(t),
		0,
	); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestPeerGreeterRejectsSeedWithoutHash(t *testing.T) {
	greeter := NewHTTPPeerGreeter(http.DefaultClient, "freeworld")
	_, err := greeter.Greet(context.Background(), "127.0.0.1:8090", yacymodel.Seed{}, 0)
	if err == nil {
		t.Fatal("expected error when self seed has no hash")
	}
}
