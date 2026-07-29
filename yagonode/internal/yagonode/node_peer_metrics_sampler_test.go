package yagonode

import "testing"

// TestObservedPeerRosterCountsWithoutASharedSampler closes the fault the v0.0.27
// memo left open. Only observePeerRosterWithClock installs a sampler, so a
// struct literal assembled anywhere else carried a nil one and took its mutex
// through that nil pointer on the very first observation, faulting the process.
// A roster without a shared sampler now counts through a private one: it cannot
// ration the all-shard read across copies of the roster, which is the only thing
// sharing buys, and it cannot crash.
func TestObservedPeerRosterCountsWithoutASharedSampler(t *testing.T) {
	t.Parallel()

	roster := &persistedPeerCountRoster{
		countingPeerRoster: countingPeerRoster{known: 7, reachable: 3},
	}
	metrics := &recordingPeerMetrics{}

	observedPeerRoster{Roster: roster, observer: metrics}.observe(t.Context())

	if metrics.calls != 1 || metrics.lastKnown != 7 || metrics.lastLive != 3 {
		t.Fatalf(
			"observations = %d, known = %d, live = %d; want one call reporting 7 and 3",
			metrics.calls, metrics.lastKnown, metrics.lastLive,
		)
	}
	if roster.persistedReads != 1 {
		t.Fatalf("persisted reads = %d, want the count taken once", roster.persistedReads)
	}
}
