package yagonode

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagoproto"
)

func TestPeerAnnouncementRosterRejectsGreetEchoedSelf(t *testing.T) {
	self := helloRosterSeed(t, "self", "203.0.113.9")
	var peer yagomodel.Seed
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			response := yagoproto.HelloResponse{
				YourIP:   "203.0.113.9",
				YourType: yagomodel.PeerSenior,
				Seeds:    []yagomodel.Seed{peer, self},
			}
			_, _ = strings.NewReader(response.Encode().Encode()).WriteTo(writer)
		}),
	)
	t.Cleanup(server.Close)
	peer = peerAnnouncementServerSeed(t, server)

	roster, err := peerroster.Open(
		t.Context(),
		openTestVault(t),
		self.Hash,
		time.Now,
		peerroster.Capacity{Reservoir: 8, Active: 4},
	)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	announcer := peerannouncement.New(
		peerannouncement.Config{Client: server.Client(), NetworkName: "freeworld"},
		helloRosterStatus{networkName: "freeworld", self: self},
		nil,
		roster,
	)

	announcer.GreetDiscovered(t.Context(), peer)

	storedPeer, foundPeer := roster.PeerByHash(t.Context(), peer.Hash)
	_, foundSelf := roster.PeerByHash(t.Context(), self.Hash)
	if !foundPeer || storedPeer.Hash != peer.Hash || foundSelf ||
		roster.KnownPeerCount(t.Context()) != 1 || roster.ReachablePeerCount(t.Context()) != 1 {
		t.Fatalf(
			"roster peer/self/known/reachable = %#v/%t/%d/%d, want peer/false/1/1",
			storedPeer,
			foundSelf,
			roster.KnownPeerCount(t.Context()),
			roster.ReachablePeerCount(t.Context()),
		)
	}
}

func peerAnnouncementServerSeed(t *testing.T, server *httptest.Server) yagomodel.Seed {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	host, err := yagomodel.ParseHost(parsed.Hostname())
	if err != nil {
		t.Fatalf("parse server host: %v", err)
	}
	port, err := yagomodel.ParsePort(parsed.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	return yagomodel.Seed{
		Hash:     yagomodel.WordHash("peer"),
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(port),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}
}
