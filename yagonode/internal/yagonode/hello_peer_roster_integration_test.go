package yagonode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/peeradmission"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagoproto"
)

type helloRosterStatus struct {
	networkName string
	self        yagomodel.Seed
}

func (s helloRosterStatus) NetworkName(context.Context) string { return s.networkName }
func (s helloRosterStatus) SelfSeed(context.Context) yagomodel.Seed {
	return s.self
}
func (helloRosterStatus) Version(context.Context) string { return "v0.0.20" }
func (helloRosterStatus) Uptime(context.Context) int     { return 1 }

type helloRosterProbeTransport struct {
	reachable *bool
}

type cancelingHelloRosterProbeTransport struct {
	cancel context.CancelFunc
}

func (p cancelingHelloRosterProbeTransport) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	p.cancel()
	<-request.Context().Done()

	return nil, fmt.Errorf("cancel hello roster callback: %w", request.Context().Err())
}

func (p helloRosterProbeTransport) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	if !*p.reachable {
		return nil, errors.New("caller unreachable")
	}
	encoded := yagoproto.QueryResponse{Response: 1}.Encode().Encode()

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(encoded)),
		Request:    request,
	}, nil
}

func TestHelloAndRosterRetainPromoteAndDoNotDowngradeCaller(t *testing.T) {
	self := helloRosterSeed(t, "self", "203.0.113.1")
	roster, err := peerroster.Open(
		t.Context(), openTestVault(t), self.Hash, time.Now,
		peerroster.Capacity{Reservoir: 8, Active: 4},
	)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	status := helloRosterStatus{networkName: "freeworld", self: self}
	reachable := false
	mux := http.NewServeMux()
	peeradmission.MountHello(
		httpguard.NewWireRouter(
			mux,
			httpguard.WireGate{
				Guard: httpguard.NewRequestGuard(
					httpguard.DefaultMaxBodyBytes,
					httpguard.DefaultRequestTimeout,
				),
				Respond: httpguard.NewWireResponder(status),
				Address: httpguard.NewClientAddressResolver(nil),
			},
		),
		nodeidentity.Identity{Hash: self.Hash, NetworkName: status.networkName},
		peeradmission.HelloExchange{
			Status:       status,
			Reachability: roster,
			Client: &http.Client{
				Transport: helloRosterProbeTransport{reachable: &reachable},
			},
		},
	)
	caller := helloRosterSeed(t, "caller", "203.0.113.2")
	caller.PeerType = yagomodel.Some(yagomodel.PeerPrincipal)

	serveHelloRosterRequest(t, mux, caller)
	assertHelloRosterCaller(t, roster, caller.Hash, yagomodel.PeerJunior, false)

	reachable = true
	serveHelloRosterRequest(t, mux, caller)
	assertHelloRosterCaller(t, roster, caller.Hash, yagomodel.PeerPrincipal, true)

	reachable = false
	serveHelloRosterRequest(t, mux, caller)
	assertHelloRosterCaller(t, roster, caller.Hash, yagomodel.PeerPrincipal, true)
}

func TestHelloAndRosterRetainJuniorAfterRequestCancellation(t *testing.T) {
	self := helloRosterSeed(t, "self", "203.0.113.1")
	roster, err := peerroster.Open(
		t.Context(), openTestVault(t), self.Hash, time.Now,
		peerroster.Capacity{Reservoir: 8, Active: 4},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := helloRosterStatus{networkName: "freeworld", self: self}
	requestContext, cancel := context.WithCancel(t.Context())
	mux := http.NewServeMux()
	peeradmission.MountHello(
		httpguard.NewWireRouter(
			mux,
			httpguard.WireGate{
				Guard: httpguard.NewRequestGuard(
					httpguard.DefaultMaxBodyBytes,
					httpguard.DefaultRequestTimeout,
				),
				Respond: httpguard.NewWireResponder(status),
				Address: httpguard.NewClientAddressResolver(nil),
			},
		),
		nodeidentity.Identity{Hash: self.Hash, NetworkName: status.networkName},
		peeradmission.HelloExchange{
			Status:       status,
			Reachability: roster,
			Client: &http.Client{
				Transport: cancelingHelloRosterProbeTransport{cancel: cancel},
			},
		},
	)
	caller := helloRosterSeed(t, "caller", "203.0.113.2")
	request := yagoproto.HelloRequest{
		NetworkName: status.networkName,
		Seed:        caller,
		Iam:         caller.Hash.String(),
	}
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequestWithContext(
		requestContext,
		http.MethodGet,
		yagoproto.PathHello+"?"+request.Form().Encode(),
		nil,
	)
	started := time.Now()
	mux.ServeHTTP(recorder, httpRequest)
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("canceled hello elapsed = %v", elapsed)
	}
	assertHelloRosterCaller(t, roster, caller.Hash, yagomodel.PeerJunior, false)
}

func TestHelloPeerRosterSelectsNewestReachablePeersWithinLimit(t *testing.T) {
	self := helloRosterSeed(t, "self", "203.0.113.1")
	now := time.Unix(1_000, 0)
	roster, err := peerroster.Open(
		t.Context(),
		openTestVault(t),
		self.Hash,
		func() time.Time {
			now = now.Add(time.Second)

			return now
		},
		peerroster.Capacity{Reservoir: 8, Active: 8},
	)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	oldest := helloRosterSeed(t, "oldest", "203.0.113.2")
	middle := helloRosterSeed(t, "middle", "203.0.113.3")
	newest := helloRosterSeed(t, "newest", "203.0.113.4")
	roster.ObserveResponder(t.Context(), oldest)
	roster.ObserveResponder(t.Context(), middle)
	roster.ObserveResponder(t.Context(), newest)

	selected := (helloPeerRoster{roster: roster}).FreshestPeers(t.Context(), 2)
	if len(selected) != 2 || selected[0].Hash != newest.Hash || selected[1].Hash != middle.Hash {
		t.Fatalf("selected peers = %#v, want newest then middle", selected)
	}
}

func helloRosterSeed(
	t *testing.T,
	identity string,
	address string,
) yagomodel.Seed {
	t.Helper()
	host, err := yagomodel.ParseHost(address)
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}

	return yagomodel.Seed{
		Hash:     yagomodel.WordHash(identity),
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(yagomodel.Port(8090)),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}
}

func serveHelloRosterRequest(
	t *testing.T,
	handler http.Handler,
	caller yagomodel.Seed,
) {
	t.Helper()
	request := yagoproto.HelloRequest{
		NetworkName: "freeworld",
		Seed:        caller,
		Iam:         caller.Hash.String(),
	}
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathHello+"?"+request.Form().Encode(),
		nil,
	)
	handler.ServeHTTP(recorder, httpRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("hello status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
}

func assertHelloRosterCaller(
	t *testing.T,
	roster peerroster.Roster,
	peer yagomodel.Hash,
	wantType yagomodel.PeerType,
	wantReachable bool,
) {
	t.Helper()
	stored, found := roster.PeerByHash(t.Context(), peer)
	classification, classified := stored.PeerType.Get()
	wantReachablePeers := 0
	if wantReachable {
		wantReachablePeers = 1
	}
	reachable := roster.ReachablePeers(t.Context())
	candidates := roster.FreshestPeers(t.Context(), 8)
	if !found || !classified || classification != wantType ||
		roster.KnownPeerCount(t.Context()) != 1 ||
		roster.ReachablePeerCount(t.Context()) != wantReachablePeers ||
		len(reachable) != wantReachablePeers || len(candidates) != wantReachablePeers {
		t.Fatalf(
			"stored caller = %#v found/type %v/%v want %q known/reachable/candidates %d/%d/%d",
			stored,
			found,
			classified,
			wantType,
			roster.KnownPeerCount(t.Context()),
			roster.ReachablePeerCount(t.Context()),
			len(candidates),
		)
	}
	if wantReachable && (reachable[0].Hash != peer || candidates[0].Hash != peer) {
		t.Fatalf("reachable/candidates = %#v/%#v, want %q", reachable, candidates, peer)
	}
}
