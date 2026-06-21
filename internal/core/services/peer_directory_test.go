package services

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakePinger struct {
	err    error
	called bool
}

func (p *fakePinger) Ping(_ context.Context, _ yacymodel.Seed) error {
	p.called = true

	return p.err
}

type fakeTrustedSeeds struct {
	seeds []yacymodel.Seed
}

func (f fakeTrustedSeeds) Trusted(_ context.Context) []yacymodel.Seed {
	return f.seeds
}

func noShuffle(int, func(i, j int)) {}

func reverseShuffle(n int, swap func(i, j int)) {
	for i := 0; i < n/2; i++ {
		swap(i, n-1-i)
	}
}

func callerSeed(hash string, ip string, port int) yacymodel.Seed {
	seed := yacymodel.Seed{Hash: hashFor(hash)}
	if ip != "" {
		seed.IP = yacymodel.Some(yacymodel.Host(ip))
	}
	if port != 0 {
		seed.Port = yacymodel.Some(yacymodel.Port(port))
	}

	return seed
}

func TestHelloClassifiesCaller(t *testing.T) {
	cases := []struct {
		name     string
		seed     yacymodel.Seed
		pingErr  error
		want     yacymodel.PeerType
		wantPing bool
	}{
		{"reachable", callerSeed("a", "10.0.0.1", 8090), nil, yacymodel.PeerSenior, true},
		{
			"unreachable",
			callerSeed("a", "10.0.0.1", 8090),
			errors.New("dial failed"),
			yacymodel.PeerJunior,
			true,
		},
		{"no ip", callerSeed("b", "", 8090), nil, yacymodel.PeerJunior, false},
		{"no port", callerSeed("c", "10.0.0.1", 0), nil, yacymodel.PeerJunior, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinger := &fakePinger{err: tc.pingErr}
			dir := NewPeerDirectory(pinger, fakeTrustedSeeds{}, noShuffle)
			outcome, err := dir.Hello(context.Background(), tc.seed, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if outcome.CallerType != tc.want {
				t.Errorf("got %v, want %v", outcome.CallerType, tc.want)
			}
			if pinger.called != tc.wantPing {
				t.Errorf("pinger called = %v, want %v", pinger.called, tc.wantPing)
			}
		})
	}
}

func TestHelloAnnouncesTrustedSeedsNotCaller(t *testing.T) {
	trusted := callerSeed("trusted", "203.0.113.1", 8090)
	caller := callerSeed("caller", "10.0.0.1", 8090)
	dir := NewPeerDirectory(
		&fakePinger{},
		fakeTrustedSeeds{seeds: []yacymodel.Seed{trusted}},
		noShuffle,
	)

	outcome, err := dir.Hello(context.Background(), caller, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outcome.Known) != 1 {
		t.Fatalf("got %d known, want 1", len(outcome.Known))
	}
	if outcome.Known[0].Hash != hashFor("trusted") {
		t.Errorf("announced %q, want trusted seed", outcome.Known[0].Hash)
	}
	for _, seed := range outcome.Known {
		if seed.Hash == hashFor("caller") {
			t.Error("self-reported caller must not be redistributed")
		}
	}
}

func trustedSet(hashes ...string) []yacymodel.Seed {
	seeds := make([]yacymodel.Seed, len(hashes))
	for i, h := range hashes {
		seeds[i] = callerSeed(h, "203.0.113.1", 8090)
	}

	return seeds
}

func announcedHashes(outcome contracts.HelloOutcome) []string {
	hashes := make([]string, len(outcome.Known))
	for i, seed := range outcome.Known {
		hashes[i] = string(seed.Hash)
	}

	return hashes
}

func TestHelloLimitsToRequestedCount(t *testing.T) {
	dir := NewPeerDirectory(
		&fakePinger{},
		fakeTrustedSeeds{seeds: trustedSet("a", "b", "c")},
		noShuffle,
	)

	outcome, err := dir.Hello(context.Background(), callerSeed("caller", "10.0.0.1", 8090), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(outcome.Known); got != 2 {
		t.Fatalf("announced %d, want 2", got)
	}
}

func TestHelloCountZeroReturnsAll(t *testing.T) {
	dir := NewPeerDirectory(
		&fakePinger{},
		fakeTrustedSeeds{seeds: trustedSet("a", "b", "c")},
		noShuffle,
	)

	outcome, err := dir.Hello(context.Background(), callerSeed("caller", "10.0.0.1", 8090), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(outcome.Known); got != 3 {
		t.Fatalf("announced %d, want 3", got)
	}
}

func TestHelloSelectsViaShuffle(t *testing.T) {
	dir := NewPeerDirectory(
		&fakePinger{},
		fakeTrustedSeeds{seeds: trustedSet("a", "b", "c")},
		reverseShuffle,
	)

	outcome, err := dir.Hello(context.Background(), callerSeed("caller", "10.0.0.1", 8090), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := announcedHashes(outcome)
	want := []string{string(hashFor("c")), string(hashFor("b"))}
	if !slices.Equal(got, want) {
		t.Errorf("announced %v, want %v", got, want)
	}
}
