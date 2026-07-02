package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

type countingPeerRoster struct {
	known     int
	reachable int
}

func (r *countingPeerRoster) Discover(_ context.Context, seeds ...yacymodel.Seed) {
	r.known += len(seeds)
}

func (r *countingPeerRoster) ConfirmReachable(context.Context, yacymodel.Hash) {
	r.reachable++
}

func (r *countingPeerRoster) ConfirmUnreachable(context.Context, yacymodel.Hash) {
	r.known--
	r.reachable--
}

func (r *countingPeerRoster) RejectRemoteIndex(context.Context, yacymodel.Seed) {}

func (r *countingPeerRoster) FreshestPeers(context.Context, int) []yacymodel.Seed { return nil }

func (r *countingPeerRoster) ReachablePeers(context.Context) []yacymodel.Seed { return nil }

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

	roster.Discover(ctx, yacymodel.Seed{Hash: yacymodel.Hash("AAAAAAAAAAAA")})
	roster.ConfirmReachable(ctx, yacymodel.Hash("AAAAAAAAAAAA"))
	if observer.lastKnown != 1 || observer.lastLive != 1 {
		t.Fatalf(
			"observed counts = %d/%d, want 1/1",
			observer.lastKnown,
			observer.lastLive,
		)
	}

	roster.ConfirmUnreachable(ctx, yacymodel.Hash("AAAAAAAAAAAA"))
	if observer.lastKnown != 0 || observer.lastLive != 0 {
		t.Fatalf(
			"observed counts after drop = %d/%d, want 0/0",
			observer.lastKnown,
			observer.lastLive,
		)
	}
}
