package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

func TestObservedPeerRosterUpdatesCountsForAnsweredObservations(t *testing.T) {
	clock := newPeerRosterClock()
	observer := &recordingPeerMetrics{}
	roster := observePeerRosterWithClock(
		t.Context(), &countingPeerRoster{}, observer, clock.Now,
	)
	clock.advance(knownPeerSamplingInterval)
	endpoint := peerwire.Endpoint{Scheme: "http", Host: "192.0.2.7", Port: "8090"}

	roster.ObserveAnsweredResponder(
		t.Context(), yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")}, endpoint,
	)
	if observer.lastKnown != 1 || observer.lastLive != 1 {
		t.Fatalf(
			"answered responder counts = %d/%d, want 1/1",
			observer.lastKnown,
			observer.lastLive,
		)
	}

	clock.advance(knownPeerSamplingInterval)
	roster.ObserveAnsweredCaller(
		t.Context(),
		yagomodel.Seed{Hash: yagomodel.Hash("BBBBBBBBBBBB")},
		yagomodel.PeerSenior,
		endpoint,
	)
	if observer.lastKnown != 2 || observer.lastLive != 1 {
		t.Fatalf(
			"answered caller counts = %d/%d, want 2/1",
			observer.lastKnown,
			observer.lastLive,
		)
	}
}
