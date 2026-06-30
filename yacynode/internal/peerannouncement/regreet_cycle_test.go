package peerannouncement

import (
	"context"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type stubGreeter struct {
	result greetResult
	err    error
	calls  int
}

func (g *stubGreeter) Greet(context.Context, string, yacymodel.Seed, int) (greetResult, error) {
	g.calls++

	return g.result, g.err
}

type stubTargets struct {
	rounds [][]yacymodel.Seed
}

func (s *stubTargets) GreetTargets(context.Context) []yacymodel.Seed {
	if len(s.rounds) == 0 {
		return nil
	}
	round := s.rounds[0]
	s.rounds = s.rounds[1:]

	return round
}

type stubDiscovery struct {
	discovered []yacymodel.Seed
}

func (d *stubDiscovery) Discover(_ context.Context, seeds ...yacymodel.Seed) {
	d.discovered = append(d.discovered, seeds...)
}

type stubReachability struct {
	reachable   []yacymodel.Hash
	unreachable []yacymodel.Hash
}

func (r *stubReachability) Reachable(_ context.Context, peer yacymodel.Hash) {
	r.reachable = append(r.reachable, peer)
}

func (r *stubReachability) Unreachable(_ context.Context, peer yacymodel.Hash) {
	r.unreachable = append(r.unreachable, peer)
}

type stubSelf struct {
	seed yacymodel.Seed
}

func (s stubSelf) SelfSeed(context.Context) yacymodel.Seed {
	return s.seed
}

type stubSeedSource struct {
	seeds []yacymodel.Seed
	calls int
}

func (s *stubSeedSource) Fetch(context.Context) []yacymodel.Seed {
	s.calls++

	return s.seeds
}

func TestAnnounceRecordsReachableAndGossip(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")
	known := callerSeed(t, "known", "198.51.100.7")

	reachability := &stubReachability{}
	discovery := &stubDiscovery{}
	a := &announcer{
		self:         stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:        &stubSeedSource{},
		discovery:    discovery,
		reachability: reachability,
		targets:      &stubTargets{rounds: [][]yacymodel.Seed{{peer}}},
		greeter: &stubGreeter{result: greetResult{
			YourType: yacymodel.PeerSenior,
			Known:    []yacymodel.Seed{known},
		}},
	}

	a.Announce(ctx)

	if len(reachability.reachable) != 1 || reachability.reachable[0] != peer.Hash {
		t.Fatalf("reachable = %v, want [%v]", reachability.reachable, peer.Hash)
	}
	if len(discovery.discovered) != 1 || discovery.discovered[0].Hash != known.Hash {
		t.Fatalf("discovered = %v, want gossiped known seed", discovery.discovered)
	}
}

func TestAnnounceMarksFailedGreetUnreachable(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")

	reachability := &stubReachability{}
	a := &announcer{
		self:         stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:        &stubSeedSource{},
		discovery:    &stubDiscovery{},
		reachability: reachability,
		targets:      &stubTargets{rounds: [][]yacymodel.Seed{{peer}}},
		greeter:      &stubGreeter{err: errors.New("boom")},
	}

	a.Announce(ctx)

	if len(reachability.unreachable) != 1 || reachability.unreachable[0] != peer.Hash {
		t.Fatalf("unreachable = %v, want [%v]", reachability.unreachable, peer.Hash)
	}
	if len(reachability.reachable) != 0 {
		t.Fatalf("reachable = %v, want none on failure", reachability.reachable)
	}
}

func TestAnnounceColdStartFetchesSeedSource(t *testing.T) {
	ctx := context.Background()
	seed := callerSeed(t, "seed", "203.0.113.1")

	source := &stubSeedSource{seeds: []yacymodel.Seed{seed}}
	discovery := &stubDiscovery{}
	greeter := &stubGreeter{result: greetResult{YourType: yacymodel.PeerSenior}}
	a := &announcer{
		self:         stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:        source,
		discovery:    discovery,
		reachability: &stubReachability{},
		targets:      &stubTargets{rounds: [][]yacymodel.Seed{{}, {seed}}},
		greeter:      greeter,
	}

	a.Announce(ctx)

	if source.calls != 1 {
		t.Fatalf("seed source calls = %d, want 1 on cold start", source.calls)
	}
	if len(discovery.discovered) != 1 || discovery.discovered[0].Hash != seed.Hash {
		t.Fatalf("discovered = %v, want seed source seed", discovery.discovered)
	}
	if greeter.calls != 1 {
		t.Fatalf("greeter calls = %d, want 1 after top-up", greeter.calls)
	}
}

func TestAnnounceWarnsWhenReportedJunior(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")

	reachability := &stubReachability{}
	a := &announcer{
		self:         stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:        &stubSeedSource{},
		discovery:    &stubDiscovery{},
		reachability: reachability,
		targets:      &stubTargets{rounds: [][]yacymodel.Seed{{peer}}},
		greeter:      &stubGreeter{result: greetResult{YourType: yacymodel.PeerJunior}},
	}

	a.Announce(ctx)

	if len(reachability.reachable) != 1 {
		t.Fatalf("reachable = %v, want peer still recorded reachable", reachability.reachable)
	}
}
