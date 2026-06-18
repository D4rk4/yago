package services

import (
	"context"
	"errors"
	"strconv"
	"testing"

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

func callerSeed(hash string, ip string, port int) yacymodel.Seed {
	return yacymodel.Seed{
		yacymodel.SeedHash: string(hashFor(hash)),
		yacymodel.SeedIP:   ip,
		yacymodel.SeedPort: strconv.Itoa(port),
	}
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
			dir := NewPeerDirectory(pinger, fakeTrustedSeeds{})
			outcome, err := dir.Hello(context.Background(), tc.seed)
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
	dir := NewPeerDirectory(&fakePinger{}, fakeTrustedSeeds{seeds: []yacymodel.Seed{trusted}})

	outcome, err := dir.Hello(context.Background(), caller)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outcome.Known) != 1 {
		t.Fatalf("got %d known, want 1", len(outcome.Known))
	}
	if outcome.Known[0][yacymodel.SeedHash] != string(hashFor("trusted")) {
		t.Errorf("announced %q, want trusted seed", outcome.Known[0][yacymodel.SeedHash])
	}
	for _, seed := range outcome.Known {
		if seed[yacymodel.SeedHash] == string(hashFor("caller")) {
			t.Error("self-reported caller must not be redistributed")
		}
	}
}
