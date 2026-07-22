package peerannouncement

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type stubGreeter struct {
	result greetResult
	err    error
	calls  int
}

func (g *stubGreeter) Greet(
	_ context.Context,
	target yagomodel.Seed,
	_ yagomodel.Seed,
	_ int,
) (greetResult, error) {
	g.calls++
	result := g.result
	if result.Responder.Hash == "" {
		result.Responder = target
	}

	return result, g.err
}

type cancelingGreeter struct {
	cancel context.CancelFunc
	calls  int
}

func (g *cancelingGreeter) Greet(
	_ context.Context,
	target yagomodel.Seed,
	_ yagomodel.Seed,
	_ int,
) (greetResult, error) {
	g.calls++
	if g.calls == 2 {
		g.cancel()
	}

	return greetResult{Responder: target, YourType: yagomodel.PeerSenior}, nil
}

type stubRoster struct {
	rounds      [][]yagomodel.Seed
	discovered  []yagomodel.Seed
	responders  []yagomodel.Seed
	reachable   []yagomodel.Hash
	unreachable []yagomodel.Hash
	prunes      int
}

func (s *stubRoster) FreshestPeers(context.Context, int) []yagomodel.Seed {
	if len(s.rounds) == 0 {
		return nil
	}
	round := s.rounds[0]
	s.rounds = s.rounds[1:]

	return round
}

func (s *stubRoster) Discover(_ context.Context, seeds ...yagomodel.Seed) {
	s.discovered = append(s.discovered, seeds...)
}

func (s *stubRoster) ObserveResponder(_ context.Context, responder yagomodel.Seed) {
	s.responders = append(s.responders, responder)
	s.reachable = append(s.reachable, responder.Hash)
}

func (s *stubRoster) ConfirmReachable(_ context.Context, peer yagomodel.Hash) {
	s.reachable = append(s.reachable, peer)
}

func (s *stubRoster) ConfirmUnreachable(_ context.Context, peer yagomodel.Hash) {
	s.unreachable = append(s.unreachable, peer)
}

func (s *stubRoster) PruneExpired(context.Context) {
	s.prunes++
}

type stubSelf struct {
	seed yagomodel.Seed
}

func (s stubSelf) SelfSeed(context.Context) yagomodel.Seed {
	return s.seed
}

type stubSeedSource struct {
	seeds []yagomodel.Seed
	calls int
}

func (s *stubSeedSource) Fetch(context.Context) []yagomodel.Seed {
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

	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{result: greetResult{
			YourType: yagomodel.PeerSenior,
			Known:    []yagomodel.Seed{known},
		}},
	}

	a.Announce(ctx)

	if len(roster.reachable) != 1 || roster.reachable[0] != peer.Hash {
		t.Fatalf("reachable = %v, want [%v]", roster.reachable, peer.Hash)
	}
	if len(roster.discovered) != 1 || roster.discovered[0].Hash != known.Hash {
		t.Fatalf("discovered = %v, want gossiped known seed", roster.discovered)
	}
	if roster.prunes != 1 {
		t.Fatalf("prunes = %d, want 1", roster.prunes)
	}
}

func TestAnnounceObservesVerifiedResponderMetadata(t *testing.T) {
	peer := callerSeed(t, "peer", "203.0.113.1")
	peer.RWICount = yagomodel.Some(17)
	responder := callerSeed(t, "peer", "198.51.100.77")
	responder.Hash = peer.Hash
	responder.Port = yagomodel.Some(yagomodel.Port(9999))
	responder.PortSSL = yagomodel.Some(yagomodel.Port(9443))
	responder.Name = yagomodel.Some("current-peer")
	responder.RWICount = yagomodel.Some(8_363_840)

	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{result: greetResult{
			Responder: responder,
			YourType:  yagomodel.PeerSenior,
		}},
	}

	a.Announce(t.Context())

	if len(roster.responders) != 1 {
		t.Fatalf("responders = %v, want one", roster.responders)
	}
	stored := roster.responders[0]
	name, _ := stored.Name.Get()
	rwi, _ := stored.RWICount.Get()
	endpoint, _ := stored.NetworkAddress()
	if name != "current-peer" || rwi != 8_363_840 || endpoint != "203.0.113.1:8090" {
		t.Fatalf("verified responder = %#v, want refreshed metadata", stored)
	}
	if _, retained := stored.PortSSL.Get(); retained {
		t.Fatalf("verified responder retained uncontacted TLS port: %#v", stored)
	}
}

func TestAnnouncePersistsActualWinningAddress(t *testing.T) {
	peer := callerSeed(t, "peer", "192.0.2.7")
	alternatives, err := yagomodel.ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	peer.IP6 = yagomodel.Some(alternatives)
	responder := callerSeed(t, "peer", "198.51.100.77")
	responder.Hash = peer.Hash
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{result: greetResult{
			Responder:     responder,
			ContactedHost: yagomodel.Some(alternatives[0]),
			YourType:      yagomodel.PeerSenior,
		}},
	}

	a.Announce(t.Context())

	if len(roster.responders) != 1 {
		t.Fatalf("responders = %v, want one", roster.responders)
	}
	stored := roster.responders[0]
	address, ok := stored.NetworkAddress()
	if !ok || address != "[2001:db8::7]:8090" || stored.Hash != peer.Hash {
		t.Fatalf("stored winning endpoint = %q/%v hash=%q", address, ok, stored.Hash)
	}
	hosts := stored.AdvertisedHosts()
	if len(hosts) != 2 || hosts[0].String() != "2001:db8::7" ||
		hosts[1].String() != "192.0.2.7" {
		t.Fatalf("stored address order = %v", hosts)
	}
}

func TestAnnounceSkipsSelfInTargets(t *testing.T) {
	ctx := context.Background()
	self := callerSeed(t, "self", "203.0.113.9")

	roster := &stubRoster{rounds: [][]yagomodel.Seed{{self}}}
	greeter := &stubGreeter{result: greetResult{YourType: yagomodel.PeerSenior}}
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
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
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
	peer := yagomodel.Seed{Hash: hashFor("peer")}
	greeter := &stubGreeter{result: greetResult{YourType: yagomodel.PeerSenior}}
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
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
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{
			result: greetResult{YourType: yagomodel.PeerJunior, YourIP: "198.51.100.1"},
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
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}, {peer}}}
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

func TestGreetDiscoveredRecordsProbeFailureAndSkipsRoster(t *testing.T) {
	ctx := context.Background()
	observer := &recordingObserver{}
	roster := &stubRoster{}
	a := &announcer{
		self:     stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		roster:   roster,
		greeter:  &stubGreeter{err: errors.New("boom")},
		observer: observer,
	}

	a.GreetDiscovered(ctx, callerSeed(t, "peer", "203.0.113.1"))

	if observer.failures.Load() != 1 {
		t.Fatalf("probe failures = %d, want 1", observer.failures.Load())
	}
	if len(roster.discovered) != 0 || len(roster.reachable) != 0 {
		t.Fatalf("a failed greet must not touch the roster: %v %v",
			roster.discovered, roster.reachable)
	}
}

func TestGreetDiscoveredVerifiesBeforeAdmittingToRoster(t *testing.T) {
	ctx := context.Background()
	peer := callerSeed(t, "peer", "203.0.113.1")
	known := callerSeed(t, "known", "198.51.100.7")
	roster := &stubRoster{}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		roster: roster,
		greeter: &stubGreeter{result: greetResult{
			YourType: yagomodel.PeerSenior,
			Known:    []yagomodel.Seed{known},
		}},
	}

	a.GreetDiscovered(ctx, peer)

	if len(roster.reachable) != 1 || roster.reachable[0] != peer.Hash {
		t.Fatalf("reachable = %v, want the verified peer %v", roster.reachable, peer.Hash)
	}
	if len(roster.responders) != 1 || roster.responders[0].Hash != peer.Hash {
		t.Fatalf("responders = %v, want the verified peer", roster.responders)
	}
	if len(roster.discovered) != 1 || roster.discovered[0].Hash != known.Hash {
		t.Fatalf("discovered = %v, want only the responder gossip", roster.discovered)
	}
}

func TestRunFetchesSeedSourceOnStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seed := callerSeed(t, "seed", "203.0.113.1")
	source := &stubSeedSource{seeds: []yagomodel.Seed{seed}}
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{seed}}}
	a := &announcer{
		interval: time.Hour,
		self:     stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:    source,
		roster:   roster,
		greeter:  &stubGreeter{result: greetResult{YourType: yagomodel.PeerSenior}},
	}

	a.Run(ctx)

	if source.calls != 1 {
		t.Fatalf("seed source calls = %d, want 1 on start", source.calls)
	}
}
