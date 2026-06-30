package peeradmission

import (
	"context"
	"slices"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type stubProbe struct {
	reachable bool
}

func (p stubProbe) Reachable(context.Context, yacymodel.Seed, yacymodel.Hash, string) bool {
	return p.reachable
}

type stubReachablePeers struct {
	seeds []yacymodel.Seed
}

func (s stubReachablePeers) ReachablePeers(context.Context) []yacymodel.Seed {
	return s.seeds
}

type stubRefresher struct {
	refreshed []yacymodel.Hash
}

func (r *stubRefresher) Reachable(_ context.Context, peer yacymodel.Hash) {
	r.refreshed = append(r.refreshed, peer)
}

func noShuffle(int, func(i, j int)) {}

func reverseShuffle(n int, swap func(i, j int)) {
	for i := 0; i < n/2; i++ {
		swap(i, n-1-i)
	}
}

func directoryWith(
	t testing.TB,
	probe callerReachabilityProbe,
	refresher *stubRefresher,
	trusted ...yacymodel.Seed,
) peerDirectory {
	return newPeerDirectory(
		probe,
		stubReachablePeers{seeds: trusted},
		refresher,
		noShuffle,
		selfStatus(t),
	)
}

func TestDirectoryClassifiesReachableAddressedCallerAsSenior(t *testing.T) {
	refresher := &stubRefresher{}
	dir := directoryWith(
		t,
		stubProbe{reachable: true},
		refresher,
		callerSeed(t, "trusted", "203.0.113.1", 8090),
	)

	caller := callerSeed(t, "caller", "10.0.0.1", 8090)
	outcome, err := dir.Hello(context.Background(), caller, 0)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if outcome.CallerType != yacymodel.PeerSenior {
		t.Fatalf("CallerType = %q, want senior", outcome.CallerType)
	}
	if got := len(outcome.Known); got != 1 {
		t.Fatalf("Known = %d, want 1 (trusted)", got)
	}
	if !slices.Equal(refresher.refreshed, []yacymodel.Hash{caller.Hash}) {
		t.Fatalf("refreshed = %v, want senior caller refreshed", refresher.refreshed)
	}
}

func TestDirectoryClassifiesUnreachableCallerAsJunior(t *testing.T) {
	refresher := &stubRefresher{}
	dir := directoryWith(t, stubProbe{reachable: false}, refresher)

	outcome, err := dir.Hello(context.Background(), callerSeed(t, "caller", "10.0.0.1", 8090), 0)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if outcome.CallerType != yacymodel.PeerJunior {
		t.Fatalf("CallerType = %q, want junior", outcome.CallerType)
	}
	if len(refresher.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for junior caller", refresher.refreshed)
	}
}

func TestDirectoryClassifiesAddresslessCallerAsJunior(t *testing.T) {
	refresher := &stubRefresher{}
	dir := directoryWith(t, stubProbe{reachable: true}, refresher)

	outcome, err := dir.Hello(context.Background(), callerSeed(t, "caller", "", 0), 0)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if outcome.CallerType != yacymodel.PeerJunior {
		t.Fatalf("CallerType = %q, want junior for addressless caller", outcome.CallerType)
	}
	if len(refresher.refreshed) != 0 {
		t.Fatalf("refreshed = %v, want no refresh for addressless caller", refresher.refreshed)
	}
}

func TestSampleSeedsLimitsAndShuffles(t *testing.T) {
	seeds := []yacymodel.Seed{
		callerSeed(t, "a", "", 0),
		callerSeed(t, "b", "", 0),
		callerSeed(t, "c", "", 0),
	}
	dir := newPeerDirectory(
		stubProbe{},
		stubReachablePeers{},
		&stubRefresher{},
		reverseShuffle,
		stubStatus{},
	)

	picked := dir.sampleSeeds(seeds, 2)

	got := []yacymodel.Hash{picked[0].Hash, picked[1].Hash}
	want := []yacymodel.Hash{hashFor("c"), hashFor("b")}
	if !slices.Equal(got, want) {
		t.Fatalf("picked = %v, want %v", got, want)
	}
}

func TestSampleSeedsCountZeroReturnsAll(t *testing.T) {
	seeds := []yacymodel.Seed{callerSeed(t, "a", "", 0), callerSeed(t, "b", "", 0)}
	dir := newPeerDirectory(
		stubProbe{},
		stubReachablePeers{},
		&stubRefresher{},
		noShuffle,
		stubStatus{},
	)

	if got := len(dir.sampleSeeds(seeds, 0)); got != 2 {
		t.Fatalf("picked = %d, want 2", got)
	}
}
