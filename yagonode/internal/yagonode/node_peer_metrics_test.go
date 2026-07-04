package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type countingPeerRoster struct {
	known     int
	reachable int
}

func (r *countingPeerRoster) Discover(_ context.Context, seeds ...yagomodel.Seed) {
	r.known += len(seeds)
}

func (r *countingPeerRoster) ConfirmReachable(context.Context, yagomodel.Hash) {
	r.reachable++
}

func (r *countingPeerRoster) ConfirmUnreachable(context.Context, yagomodel.Hash) {
	r.known--
	r.reachable--
}

func (r *countingPeerRoster) RejectRemoteIndex(context.Context, yagomodel.Seed) {}

func (r *countingPeerRoster) FreshestPeers(context.Context, int) []yagomodel.Seed { return nil }

func (r *countingPeerRoster) ReachablePeers(context.Context) []yagomodel.Seed { return nil }

func (r *countingPeerRoster) KnownPeerCount(context.Context) int {
	return r.known
}

func (r *countingPeerRoster) ReachablePeerCount(context.Context) int {
	return r.reachable
}

type recordingPeerMetrics struct {
	calls     int
	lastKnown int
	lastLive  int
}

func (m *recordingPeerMetrics) ObservePeerRoster(known, active int) {
	m.calls++
	m.lastKnown = known
	m.lastLive = active
}

func TestObservePeerRosterReturnsOriginalWithoutObserver(t *testing.T) {
	roster := &countingPeerRoster{}

	if got := observePeerRoster(context.Background(), roster, nil); got != roster {
		t.Fatalf("roster = %T, want original", got)
	}
}

func TestObservedPeerRosterUpdatesCounts(t *testing.T) {
	ctx := context.Background()
	observer := &recordingPeerMetrics{}
	roster := observePeerRoster(ctx, &countingPeerRoster{}, observer)

	if observer.calls != 1 {
		t.Fatalf("initial observations = %d, want 1", observer.calls)
	}

	roster.Discover(ctx, yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")})
	roster.ConfirmReachable(ctx, yagomodel.Hash("AAAAAAAAAAAA"))
	if observer.lastKnown != 1 || observer.lastLive != 1 {
		t.Fatalf(
			"observed counts = %d/%d, want 1/1",
			observer.lastKnown,
			observer.lastLive,
		)
	}

	roster.ConfirmUnreachable(ctx, yagomodel.Hash("AAAAAAAAAAAA"))
	if observer.lastKnown != 0 || observer.lastLive != 0 {
		t.Fatalf(
			"observed counts after drop = %d/%d, want 0/0",
			observer.lastKnown,
			observer.lastLive,
		)
	}
}
