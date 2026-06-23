package peering

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

func noShuffle(int, func(i, j int)) {}

func reverseShuffle(n int, swap func(i, j int)) {
	for i := 0; i < n/2; i++ {
		swap(i, n-1-i)
	}
}

func directoryWith(
	t testing.TB,
	probe callerReachabilityProbe,
	trusted ...yacymodel.Seed,
) peerDirectory {
	registry := NewTrustedSeeds(10)
	for _, seed := range trusted {
		registry.Absorb(context.Background(), seed)
	}

	return newPeerDirectory(probe, registry, noShuffle, selfStatus(t))
}

func TestDirectoryClassifiesReachableAddressedCallerAsSenior(t *testing.T) {
	dir := directoryWith(
		t,
		stubProbe{reachable: true},
		callerSeed(t, "trusted", "203.0.113.1", 8090),
	)

	outcome, err := dir.Hello(context.Background(), callerSeed(t, "caller", "10.0.0.1", 8090), 0)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if outcome.CallerType != yacymodel.PeerSenior {
		t.Fatalf("CallerType = %q, want senior", outcome.CallerType)
	}
	if got := len(outcome.Known); got != 1 {
		t.Fatalf("Known = %d, want 1 (trusted)", got)
	}
}

func TestDirectoryClassifiesUnreachableCallerAsJunior(t *testing.T) {
	dir := directoryWith(t, stubProbe{reachable: false})

	outcome, err := dir.Hello(context.Background(), callerSeed(t, "caller", "10.0.0.1", 8090), 0)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if outcome.CallerType != yacymodel.PeerJunior {
		t.Fatalf("CallerType = %q, want junior", outcome.CallerType)
	}
}

func TestDirectoryClassifiesAddresslessCallerAsJunior(t *testing.T) {
	dir := directoryWith(t, stubProbe{reachable: true})

	outcome, err := dir.Hello(context.Background(), callerSeed(t, "caller", "", 0), 0)
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if outcome.CallerType != yacymodel.PeerJunior {
		t.Fatalf("CallerType = %q, want junior for addressless caller", outcome.CallerType)
	}
}

func TestSampleSeedsLimitsAndShuffles(t *testing.T) {
	seeds := []yacymodel.Seed{
		callerSeed(t, "a", "", 0),
		callerSeed(t, "b", "", 0),
		callerSeed(t, "c", "", 0),
	}
	dir := newPeerDirectory(stubProbe{}, NewTrustedSeeds(10), reverseShuffle, stubStatus{})

	picked := dir.sampleSeeds(seeds, 2)

	got := []yacymodel.Hash{picked[0].Hash, picked[1].Hash}
	want := []yacymodel.Hash{hashFor("c"), hashFor("b")}
	if !slices.Equal(got, want) {
		t.Fatalf("picked = %v, want %v", got, want)
	}
}

func TestSampleSeedsCountZeroReturnsAll(t *testing.T) {
	seeds := []yacymodel.Seed{callerSeed(t, "a", "", 0), callerSeed(t, "b", "", 0)}
	dir := newPeerDirectory(stubProbe{}, NewTrustedSeeds(10), noShuffle, stubStatus{})

	if got := len(dir.sampleSeeds(seeds, 0)); got != 2 {
		t.Fatalf("picked = %d, want 2", got)
	}
}
