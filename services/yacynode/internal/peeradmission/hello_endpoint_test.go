package peeradmission

import (
	"context"
	"slices"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type stubProbe struct {
	reachable bool
	called    bool
}

func (p *stubProbe) Reachable(context.Context, yacymodel.Seed, yacymodel.Hash, string) bool {
	p.called = true

	return p.reachable
}

type stubReachability struct {
	seeds     []yacymodel.Seed
	refreshed []yacymodel.Hash
}

func (s *stubReachability) ReachablePeers(context.Context) []yacymodel.Seed {
	return s.seeds
}

func (s *stubReachability) ConfirmReachable(_ context.Context, peer yacymodel.Hash) {
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

func helloRequest(network string, caller yacymodel.Seed, count int) yacyproto.HelloRequest {
	return yacyproto.HelloRequest{
		NetworkName: network,
		Seed:        caller,
		Iam:         caller.Hash,
		Count:       count,
	}
}

func TestHelloClassifiesReachableAddressedCallerAsSenior(t *testing.T) {
	reachability := &stubReachability{
		seeds: []yacymodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)},
	}
	endpoint := newEndpoint(t, &stubProbe{reachable: true}, reachability)

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	resp, err := endpoint.Serve(context.Background(), helloRequest("freeworld", caller, 0))
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.YourType != yacymodel.PeerSenior {
		t.Fatalf("YourType = %q, want senior", resp.YourType)
	}
	if got := len(resp.Seeds); got != 2 {
		t.Fatalf("Seeds = %d, want 2 (self + trusted)", got)
	}
	if resp.Seeds[0].Hash != hashFor("self") {
		t.Fatalf("first seed = %q, want self", resp.Seeds[0].Hash)
	}
	if !slices.Equal(reachability.refreshed, []yacymodel.Hash{caller.Hash}) {
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
	if resp.YourType != yacymodel.PeerJunior {
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
	if resp.YourType != yacymodel.PeerJunior {
		t.Fatalf("YourType = %q, want junior for addressless caller", resp.YourType)
	}
	if probe.called {
		t.Fatal("probe consulted for addressless caller")
	}
	if len(reachability.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for addressless caller", reachability.refreshed)
	}
}

func TestHelloOnForeignNetworkOmitsAdmission(t *testing.T) {
	probe := &stubProbe{reachable: true}
	endpoint := newEndpoint(
		t,
		probe,
		&stubReachability{seeds: []yacymodel.Seed{callerSeed(t, "trusted", "203.0.113.1", 8090)}},
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
	reachability := &stubReachability{seeds: []yacymodel.Seed{
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
	known := []yacymodel.Hash{hashFor("a"), hashFor("b"), hashFor("c")}
	for _, seed := range resp.Seeds[1:] {
		if !slices.Contains(known, seed.Hash) {
			t.Fatalf("known peer %q not from roster %v", seed.Hash, known)
		}
	}
}

func TestHelloCountZeroReturnsAllKnownPeers(t *testing.T) {
	reachability := &stubReachability{seeds: []yacymodel.Seed{
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
