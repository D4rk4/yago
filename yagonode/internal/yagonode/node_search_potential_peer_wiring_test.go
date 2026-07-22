package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func (r *countingPeerRoster) ObservePotential(
	context.Context,
	yagomodel.Seed,
) {
	r.known++
}

func TestPotentialPeerObservationCrossesRosterWrappers(t *testing.T) {
	base := &countingPeerRoster{}
	metrics := &recordingPeerMetrics{}
	observed := observePeerRoster(t.Context(), base, metrics)
	wrapped := newBlockingRoster(observed, newFakePeerBlocks())
	potential, ok := wrapped.(interface {
		ObservePotential(context.Context, yagomodel.Seed)
	})
	if !ok {
		t.Fatalf("wrapped roster type %T has no potential observer", wrapped)
	}
	potential.ObservePotential(t.Context(), yagomodel.Seed{
		Hash: yagomodel.Hash("AAAAAAAAAAAA"),
	})
	if base.known != 1 || metrics.lastKnown != 1 {
		t.Fatalf("known = %d, observed = %d", base.known, metrics.lastKnown)
	}
}

func TestPotentialPeerObservationSkipsUnsupportedRoster(t *testing.T) {
	observed := observedPeerRoster{Roster: fakeRoster{}}
	observed.ObservePotential(t.Context(), yagomodel.Seed{})
	blocked := blockingRoster{Roster: fakeRoster{}, blocks: newFakePeerBlocks()}
	blocked.ObservePotential(t.Context(), yagomodel.Seed{})
}
