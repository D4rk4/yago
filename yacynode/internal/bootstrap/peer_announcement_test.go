package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type stubStatus struct {
	seed yacymodel.Seed
}

func (s stubStatus) SelfSeed(context.Context) yacymodel.Seed {
	return s.seed
}

type recordingSink struct {
	seeds []yacymodel.Seed
}

func (r *recordingSink) Absorb(_ context.Context, seeds ...yacymodel.Seed) {
	r.seeds = append(r.seeds, seeds...)
}

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
) (greetResult, error) {
	g.endpoints = append(g.endpoints, endpoint)
	if g.err != nil {
		return greetResult{}, g.err
	}

	return greetResult{Known: g.known}, nil
}

func TestAnnounceGreetsDiscoveredEndpoints(t *testing.T) {
	greeter := &fakeGreeter{known: []yacymodel.Seed{callerSeed(t, "known", "198.51.100.1", 8090)}}
	sink := &recordingSink{}
	announcement := newPeerAnnouncement(
		BootstrapSettings{SeedlistURLs: []string{"http://list"}},
		fakeFetcher{seeds: map[string][]yacymodel.Seed{
			"http://list": {callerSeed(t, "disc", "203.0.113.6", 8090)},
		}},
		greeter,
		stubStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		sink,
	)

	announcement.Announce(context.Background())

	if want := []string{"203.0.113.6:8090"}; len(greeter.endpoints) != len(want) ||
		greeter.endpoints[0] != want[0] {
		t.Fatalf("greeted %v, want %v", greeter.endpoints, want)
	}
	if len(sink.seeds) != 2 {
		t.Fatalf("absorbed %d, want 2 (discovered + greet-known)", len(sink.seeds))
	}
}

func TestAnnounceContinuesWhenSeedlistFails(t *testing.T) {
	greeter := &fakeGreeter{}
	announcement := newPeerAnnouncement(
		BootstrapSettings{SeedlistURLs: []string{"http://list"}},
		fakeFetcher{err: errors.New("offline")},
		greeter,
		stubStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		&recordingSink{},
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
	announcement := newPeerAnnouncement(
		BootstrapSettings{SeedlistURLs: []string{"http://list"}},
		fakeFetcher{seeds: map[string][]yacymodel.Seed{"http://list": seeds}},
		greeter,
		stubStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		&recordingSink{},
	)

	announcement.Announce(context.Background())

	if len(greeter.endpoints) != announceMaxGreets {
		t.Fatalf("greeted %d, want cap of %d", len(greeter.endpoints), announceMaxGreets)
	}
}

func TestModuleRunStopsOnContextCancel(t *testing.T) {
	module := NewAnnouncer(
		http.DefaultClient,
		"freeworld",
		BootstrapSettings{AnnounceInterval: time.Hour},
		stubStatus{seed: callerSeed(t, "self", "203.0.113.9", 8090)},
		&recordingSink{},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		module.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
