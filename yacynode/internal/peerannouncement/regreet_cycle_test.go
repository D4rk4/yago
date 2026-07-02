package peerannouncement

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
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

type cancelingGreeter struct {
	cancel context.CancelFunc
	calls  int
}

func (g *cancelingGreeter) Greet(
	context.Context,
	string,
	yacymodel.Seed,
	int,
) (greetResult, error) {
	g.calls++
	if g.calls == 2 {
		g.cancel()
	}

	return greetResult{YourType: yacymodel.PeerSenior}, nil
}

type stubRoster struct {
	rounds      [][]yacymodel.Seed
	discovered  []yacymodel.Seed
	reachable   []yacymodel.Hash
	unreachable []yacymodel.Hash
}

func (s *stubRoster) FreshestPeers(context.Context, int) []yacymodel.Seed {
	if len(s.rounds) == 0 {
		return nil
	}
	round := s.rounds[0]
	s.rounds = s.rounds[1:]

	return round
}

func (s *stubRoster) Discover(_ context.Context, seeds ...yacymodel.Seed) {
	s.discovered = append(s.discovered, seeds...)
}

func (s *stubRoster) ConfirmReachable(_ context.Context, peer yacymodel.Hash) {
	s.reachable = append(s.reachable, peer)
}

func (s *stubRoster) ConfirmUnreachable(_ context.Context, peer yacymodel.Hash) {
	s.unreachable = append(s.unreachable, peer)
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

type recordingObserver struct {
	failures atomic.Int32
}

func (o *recordingObserver) ObservePeerProbeFailure() {
	o.failures.Add(1)
}

func TestAnnounceRecordsReachableAndGossip(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")
	known := callerSeed(t, "known", "198.51.100.7")

	roster := &stubRoster{rounds: [][]yacymodel.Seed{{peer}}}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{result: greetResult{
			YourType: yacymodel.PeerSenior,
			Known:    []yacymodel.Seed{known},
		}},
	}

	a.Announce(ctx)

	if len(roster.reachable) != 1 || roster.reachable[0] != peer.Hash {
		t.Fatalf("reachable = %v, want [%v]", roster.reachable, peer.Hash)
	}
	if len(roster.discovered) != 1 || roster.discovered[0].Hash != known.Hash {
		t.Fatalf("discovered = %v, want gossiped known seed", roster.discovered)
	}
}

func TestAnnounceSkipsSelfInTargets(t *testing.T) {
	ctx := context.Background()
	self := callerSeed(t, "self", "203.0.113.9")

	roster := &stubRoster{rounds: [][]yacymodel.Seed{{self}}}
	greeter := &stubGreeter{result: greetResult{YourType: yacymodel.PeerSenior}}
	a := &announcer{
		self:    stubSelf{seed: self},
		seeds:   &stubSeedSource{},
		roster:  roster,
		greeter: greeter,
	}

	a.Announce(ctx)

	if greeter.calls != 0 {
		t.Fatalf("greeter calls = %d, want 0 when only self is a target", greeter.calls)
	}
	if len(roster.reachable) != 0 {
		t.Fatalf("reachable = %v, want none for self", roster.reachable)
	}
}

func TestAnnounceMarksFailedGreetUnreachable(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")

	observer := &recordingObserver{}
	roster := &stubRoster{rounds: [][]yacymodel.Seed{{peer}}}
	a := &announcer{
		self:     stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:    &stubSeedSource{},
		roster:   roster,
		greeter:  &stubGreeter{err: errors.New("boom")},
		observer: observer,
	}

	a.Announce(ctx)

	if len(roster.unreachable) != 1 || roster.unreachable[0] != peer.Hash {
		t.Fatalf("unreachable = %v, want [%v]", roster.unreachable, peer.Hash)
	}
	if len(roster.reachable) != 0 {
		t.Fatalf("reachable = %v, want none on failure", roster.reachable)
	}
	if observer.failures.Load() != 1 {
		t.Fatalf("probe failures = %d, want 1", observer.failures.Load())
	}
}

func TestAnnounceSkipsAddresslessTargets(t *testing.T) {
	ctx := context.Background()
	peer := yacymodel.Seed{Hash: hashFor("peer")}
	greeter := &stubGreeter{result: greetResult{YourType: yacymodel.PeerSenior}}
	roster := &stubRoster{rounds: [][]yacymodel.Seed{{peer}}}
	a := &announcer{
		self:    stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:   &stubSeedSource{},
		roster:  roster,
		greeter: greeter,
	}

	a.Announce(ctx)

	if greeter.calls != 0 {
		t.Fatalf("greeter calls = %d, want 0 for addressless target", greeter.calls)
	}
	if len(roster.reachable) != 0 || len(roster.unreachable) != 0 {
		t.Fatalf(
			"roster updates = reachable %v unreachable %v, want none",
			roster.reachable,
			roster.unreachable,
		)
	}
}

func TestAnnounceRecordsReachableWhenPeerReportsJunior(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")
	roster := &stubRoster{rounds: [][]yacymodel.Seed{{peer}}}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{
			result: greetResult{YourType: yacymodel.PeerJunior, YourIP: "198.51.100.1"},
		},
	}

	a.Announce(ctx)

	if len(roster.reachable) != 1 || roster.reachable[0] != peer.Hash {
		t.Fatalf("reachable = %v, want [%v]", roster.reachable, peer.Hash)
	}
}

func TestNewReturnsAnnouncer(t *testing.T) {
	announced := New(
		Config{
			Client:         http.DefaultClient,
			NetworkName:    "freeworld",
			Interval:       time.Hour,
			GreetsPerCycle: 3,
		},
		stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		&stubSeedSource{},
		&stubRoster{},
	)

	if _, ok := announced.(*announcer); !ok {
		t.Fatalf("New returned %T, want *announcer", announced)
	}
}

func TestNewDefaultsObserver(t *testing.T) {
	announced := New(
		Config{},
		stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		&stubSeedSource{},
		&stubRoster{},
	)

	got := announced.(*announcer)
	if got.observer != nil {
		t.Fatalf("observer = %T, want nil", got.observer)
	}
}

func TestRunAnnouncesOnTicker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	peer := callerSeed(t, "peer", "203.0.113.1")
	roster := &stubRoster{rounds: [][]yacymodel.Seed{{peer}, {peer}}}
	greeter := &cancelingGreeter{cancel: cancel}
	a := &announcer{
		interval: time.Millisecond,
		self:     stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:    &stubSeedSource{},
		roster:   roster,
		greeter:  greeter,
	}

	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("Run did not stop after ticker announce")
	}
	if greeter.calls < 2 {
		t.Fatalf("greeter calls = %d, want at least 2", greeter.calls)
	}
}

func TestRunFetchesSeedSourceOnStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seed := callerSeed(t, "seed", "203.0.113.1")
	source := &stubSeedSource{seeds: []yacymodel.Seed{seed}}
	roster := &stubRoster{rounds: [][]yacymodel.Seed{{seed}}}
	a := &announcer{
		interval: time.Hour,
		self:     stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:    source,
		roster:   roster,
		greeter:  &stubGreeter{result: greetResult{YourType: yacymodel.PeerSenior}},
	}

	a.Run(ctx)

	if source.calls != 1 {
		t.Fatalf("seed source calls = %d, want 1 on start", source.calls)
	}
}
