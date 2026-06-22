package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakeBootstrapConfig struct {
	seedlists []string
}

func (c fakeBootstrapConfig) SeedlistURLs() []string          { return c.seedlists }
func (c fakeBootstrapConfig) AnnounceInterval() time.Duration { return time.Hour }

type fakeFetcher struct {
	seeds map[string][]yacymodel.Seed
	err   error
}

func (f fakeFetcher) Fetch(_ context.Context, url string) ([]yacymodel.Seed, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.seeds[url], nil
}

type fakeGreeter struct {
	endpoints []string
	known     []yacymodel.Seed
	err       error
}

func (g *fakeGreeter) Greet(
	_ context.Context,
	endpoint string,
	_ yacymodel.Seed,
	_ int,
) (ports.GreetResult, error) {
	g.endpoints = append(g.endpoints, endpoint)
	if g.err != nil {
		return ports.GreetResult{}, g.err
	}

	return ports.GreetResult{Known: g.known}, nil
}

type fakeStatus struct {
	seed yacymodel.Seed
}

func (s fakeStatus) Snapshot(_ context.Context) contracts.StatusSnapshot {
	return contracts.StatusSnapshot{Seed: s.seed}
}

func TestAnnounceGreetsDiscoveredEndpoints(t *testing.T) {
	greeter := &fakeGreeter{known: []yacymodel.Seed{callerSeed(t, "known", "198.51.100.1", 8090)}}
	reg := NewTrustedSeedRegistry(100)
	announcement := NewPeerAnnouncement(
		fakeBootstrapConfig{
			seedlists: []string{"http://list"},
		},
		fakeFetcher{seeds: map[string][]yacymodel.Seed{
			"http://list": {callerSeed(t, "disc", "203.0.113.6", 8090)},
		}},
		greeter,
		fakeStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		reg,
	)

	announcement.Announce(context.Background())

	wantEndpoints := []string{"203.0.113.6:8090"}
	if len(greeter.endpoints) != len(wantEndpoints) {
		t.Fatalf("greeted %v, want %v", greeter.endpoints, wantEndpoints)
	}

	trusted := reg.Trusted(context.Background())
	if len(trusted) != 2 {
		t.Fatalf("trusted %d, want 2 (discovered seed + greet-known seed)", len(trusted))
	}
}

func TestAnnounceContinuesWhenSeedlistFails(t *testing.T) {
	greeter := &fakeGreeter{}
	reg := NewTrustedSeedRegistry(100)
	announcement := NewPeerAnnouncement(
		fakeBootstrapConfig{
			seedlists: []string{"http://list"},
		},
		fakeFetcher{err: errors.New("offline")},
		greeter,
		fakeStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		reg,
	)

	announcement.Announce(context.Background())

	if len(greeter.endpoints) != 0 {
		t.Fatalf("greeted %v, want none", greeter.endpoints)
	}
}

func TestAnnounceCapsGreetCount(t *testing.T) {
	seeds := make([]yacymodel.Seed, announceMaxGreets+5)
	for i := range seeds {
		seeds[i] = callerSeed(t, string(rune('a'+i)), "203.0.113.6", 8090+i)
	}
	greeter := &fakeGreeter{}
	announcement := NewPeerAnnouncement(
		fakeBootstrapConfig{seedlists: []string{"http://list"}},
		fakeFetcher{seeds: map[string][]yacymodel.Seed{"http://list": seeds}},
		greeter,
		fakeStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		NewTrustedSeedRegistry(100),
	)

	announcement.Announce(context.Background())

	if len(greeter.endpoints) != announceMaxGreets {
		t.Fatalf("greeted %d, want cap of %d", len(greeter.endpoints), announceMaxGreets)
	}
}
