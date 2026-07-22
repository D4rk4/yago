package peerannouncement

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type blockingBootstrapSource struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (s *blockingBootstrapSource) Fetch(context.Context) []yagomodel.Seed {
	s.calls.Add(1)
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release

	return nil
}

type emptyBootstrapRoster struct{}

func (emptyBootstrapRoster) Discover(context.Context, ...yagomodel.Seed)         {}
func (emptyBootstrapRoster) ObserveResponder(context.Context, yagomodel.Seed)    {}
func (emptyBootstrapRoster) ConfirmReachable(context.Context, yagomodel.Hash)    {}
func (emptyBootstrapRoster) ConfirmUnreachable(context.Context, yagomodel.Hash)  {}
func (emptyBootstrapRoster) FreshestPeers(context.Context, int) []yagomodel.Seed { return nil }

func TestEmptyRosterBootstrapRefreshIsSingleFlight(t *testing.T) {
	source := &blockingBootstrapSource{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	a := &announcer{
		self:      stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:     source,
		roster:    emptyBootstrapRoster{},
		bootstrap: bootstrapRefresh{now: time.Now, cooldown: bootstrapRetryCooldown},
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			a.Announce(t.Context())
		}()
	}
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("bootstrap refresh did not start")
	}
	time.Sleep(20 * time.Millisecond)
	if source.calls.Load() != 1 {
		t.Fatalf("concurrent bootstrap fetches = %d", source.calls.Load())
	}
	close(source.release)
	group.Wait()
}

func TestEmptyRosterBootstrapRefreshHonorsCooldown(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	source := &stubSeedSource{}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  source,
		roster: emptyBootstrapRoster{},
		bootstrap: bootstrapRefresh{
			now:      func() time.Time { return now },
			cooldown: bootstrapRetryCooldown,
		},
	}
	a.Announce(t.Context())
	a.Announce(t.Context())
	if source.calls != 1 {
		t.Fatalf("bootstrap calls during cooldown = %d", source.calls)
	}
	now = now.Add(bootstrapRetryCooldown)
	a.Announce(t.Context())
	if source.calls != 2 {
		t.Fatalf("bootstrap calls after cooldown = %d", source.calls)
	}
}
