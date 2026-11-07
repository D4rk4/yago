package yagonode

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagoegress"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagoproto"
)

func TestGreetedPeerAnsweredEndpointNeverBypassesEgressPolicy(t *testing.T) {
	self := helloRosterSeed(t, "self", "203.0.113.9")
	var (
		peer     yagomodel.Seed
		requests atomic.Int32
	)
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			response := yagoproto.HelloResponse{
				YourIP:   "203.0.113.9",
				YourType: yagomodel.PeerSenior,
				Seeds:    []yagomodel.Seed{peer},
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
	greetWith := func(client *http.Client) {
		peerannouncement.New(
			peerannouncement.Config{Client: client, NetworkName: "freeworld"},
			helloRosterStatus{networkName: "freeworld", self: self},
			nil,
			roster,
		).GreetDiscovered(t.Context(), peer)
	}

	greetWith(server.Client())
	answered, found, err := roster.AnsweredEndpoint(t.Context(), peer.Hash)
	origin, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if err != nil || !found || answered != peerwire.EndpointAt(origin) || requests.Load() != 1 {
		t.Fatalf("answered endpoint = %+v/%t/%v after %d requests",
			answered, found, err, requests.Load())
	}

	greetWith(newGuardedEgressClient(yagoegress.NewGuard(false)))
	if requests.Load() != 1 {
		t.Fatalf("guarded greet reached remembered loopback endpoint: %d requests", requests.Load())
	}
}

func assertHelloRosterAnsweredEndpoint(
	t *testing.T,
	roster peerroster.Roster,
	peer yagomodel.Hash,
	wantAnswered bool,
) {
	t.Helper()
	answered, found, err := roster.AnsweredEndpoint(t.Context(), peer)
	want := peerwire.Endpoint{}
	if wantAnswered {
		want = peerwire.Endpoint{Scheme: "http", Host: "203.0.113.2", Port: "8090"}
	}
	if err != nil || found != wantAnswered || answered != want {
		t.Fatalf("caller answered endpoint = %+v/%t/%v, want %+v", answered, found, err, want)
	}
}
