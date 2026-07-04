package peeradmission

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type stubProbe struct {
	reachable bool
	called    bool
}

func (p *stubProbe) Reachable(context.Context, yagomodel.Seed, yagomodel.Hash, string) bool {
	p.called = true

	return p.reachable
}

type stubReachability struct {
	seeds     []yagomodel.Seed
	refreshed []yagomodel.Hash
}

func (s *stubReachability) ReachablePeers(context.Context) []yagomodel.Seed {
	return s.seeds
}

func (s *stubReachability) ConfirmReachable(_ context.Context, peer yagomodel.Hash) {
	s.refreshed = append(s.refreshed, peer)
}

func newEndpoint(
	t testing.TB,
	probe callerReachabilityProbe,
	reachability *stubReachability,
) helloEndpoint {
	return helloEndpoint{
		identity:     localPeer(),
		status:       selfStatus(t),
		probe:        probe,
		reachability: reachability,
	}
}

func helloRequest(network string, caller yagomodel.Seed, count int) yagoproto.HelloRequest {
	return yagoproto.HelloRequest{
		NetworkName: network,
		Seed:        caller,
		Iam:         caller.Hash,
		Count:       count,
	}
}

func TestHelloClassifiesReachableAddressedCallerAsSenior(t *testing.T) {
	reachability := &stubReachability{
		seeds: []yagomodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)},
	}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	resp, err := endpoint.Serve(context.Background(), helloRequest("freeworld", caller, 0))
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior", resp.YourType)
	}
	if got := len(resp.Seeds); got != 2 {
		t.Fatalf("Seeds = %d, want 2 (self + trusted)", got)
	}
	if resp.Seeds[0].Hash != hashFor("self") {
		t.Fatalf("first seed = %q, want self", resp.Seeds[0].Hash)
	}
	if !slices.Equal(reachability.refreshed, []yagomodel.Hash{caller.Hash}) {
		t.Fatalf("refreshed = %v, want senior caller refreshed", reachability.refreshed)
	}
}

func TestHelloClassifiesUnreachableCallerAsJunior(t *testing.T) {
	reachability := &stubReachability{}
	endpoint := newEndpoint(t, &stubProbe{reachable: false}, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior", resp.YourType)
	}
	if len(reachability.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for junior caller", reachability.refreshed)
	}
}

func TestHelloClassifiesAddresslessCallerAsJunior(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "", 0), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior for addressless caller", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for addressless caller")
	}
	if len(reachability.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for addressless caller", reachability.refreshed)
	}
}

func TestHelloRejectsCallerUsingSelfHash(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "self", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerVirgin {
		t.Fatalf("YourType = %q, want virgin for self hash", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for caller using self hash")
	}
	if len(reachability.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for self hash", reachability.refreshed)
	}
}

func TestHelloRejectsCallerUsingSelfEndpoint(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "203.0.113.9", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerVirgin {
		t.Fatalf("YourType = %q, want virgin for self endpoint", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for caller using self endpoint")
	}
	if len(reachability.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for self endpoint", reachability.refreshed)
	}
}

func TestHelloAcceptsSameHostOnDifferentPort(t *testing.T) {
	reachability := &stubReachability{}
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(t, probe, reachability)
	caller := callerSeed(t, "caller", "203.0.113.9", 8091)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", caller, 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yagomodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior for different port", resp.YourType)
	}
	if !probe.called {
		t.Fatal("probe was not consulted for different-port caller")
	}
	if !slices.Equal(reachability.refreshed, []yagomodel.Hash{caller.Hash}) {
		t.Fatalf("refreshed = %v, want caller refreshed", reachability.refreshed)
	}
}

func TestHelloOnForeignNetworkOmitsAdmission(t *testing.T) {
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(
		t,
		probe,
		&stubReachability{seeds: []yagomodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)}},
	)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("otherworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got := len(resp.Seeds); got != 1 {
		t.Fatalf("Seeds = %d, want 1 (self only)", got)
	}
	if probe.called {
		t.Fatal("probe consulted despite foreign network")
	}
}

func TestHelloLimitsKnownPeersToCount(t *testing.T) {
	reachability := &stubReachability{seeds: []yagomodel.Seed{
		callerSeed(t, "a", "203.0.113.1", 8090),
		callerSeed(t, "b", "203.0.113.2", 8090),
		callerSeed(t, "c", "203.0.113.3", 8090),
	}}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 2),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	if got := len(resp.Seeds); got != 3 {
		t.Fatalf("Seeds = %d, want 3 (self + two of three known)", got)
	}
	known := []yagomodel.Hash{hashFor("a"), hashFor("b"), hashFor("c")}
	for _, seed := range resp.Seeds[1:] {
		if !slices.Contains(known, seed.Hash) {
			t.Fatalf("known peer %q not from roster %v", seed.Hash, known)
		}
	}
}

func TestHelloCountZeroReturnsAllKnownPeers(t *testing.T) {
	reachability := &stubReachability{seeds: []yagomodel.Seed{
		callerSeed(t, "a", "203.0.113.1", 8090),
		callerSeed(t, "b", "203.0.113.2", 8090),
	}}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	resp, err := endpoint.Serve(
		context.Background(),
		helloRequest("freeworld", callerSeed(t, "caller", "10.0.0.1", 8090), 0),
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got := len(resp.Seeds); got != 3 {
		t.Fatalf("Seeds = %d, want 3 (self + two known)", got)
	}
}

func TestShuffleKnownPeersReturnsOnRandomError(t *testing.T) {
	saved := randomPeerIndex
	t.Cleanup(func() { randomPeerIndex = saved })
	randomPeerIndex = func(io.Reader, *big.Int) (*big.Int, error) {
		return nil, errors.New("entropy failed")
	}
	peers := []yagomodel.Seed{
		callerSeed(t, "a", "203.0.113.1", 8090),
		callerSeed(t, "b", "203.0.113.2", 8090),
	}

	shuffleKnownPeers(peers)

	if peers[0].Hash != hashFor("a") || peers[1].Hash != hashFor("b") {
		t.Fatalf("peers were shuffled despite entropy error: %#v", peers)
	}
}

type helloWireStatus struct{}

func (helloWireStatus) Version(context.Context) string { return "1.940" }
func (helloWireStatus) Uptime(context.Context) int     { return 42 }

func helloGate() httpguard.WireGate {
	return httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(4096, time.Second),
		Respond: httpguard.NewWireResponder(helloWireStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	}
}

func TestMountHelloServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	identity := localPeer()
	status := selfStatus(t)
	MountHello(
		httpguard.NewWireRouter(mux, helloGate()),
		identity,
		HelloExchange{
			Status:       status,
			Reachability: &stubReachability{},
			Client:       http.DefaultClient,
		},
	)
	req := yagoproto.HelloRequest{
		NetworkName: "otherworld",
		Seed:        callerSeed(t, "caller", "10.0.0.1", 8090),
		Iam:         hashFor("caller"),
	}

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathHello+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
