package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type callerRecordingRoster struct {
	reachableRoster
	caller         yagomodel.Seed
	classification yagomodel.PeerType
}

func (r *callerRecordingRoster) ObserveCaller(
	_ context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) {
	r.caller = caller
	r.classification = classification
}

func TestHelloPeerRosterHonorsBoundsAndForwardsCaller(t *testing.T) {
	caller := yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")}
	roster := &callerRecordingRoster{}
	adapter := helloPeerRoster{roster: roster}

	if peers := adapter.FreshestPeers(t.Context(), 0); peers != nil {
		t.Fatalf("zero-limit peers = %+v", peers)
	}
	adapter.ObserveCaller(t.Context(), caller, yagomodel.PeerJunior)
	if roster.caller.Hash != caller.Hash || roster.classification != yagomodel.PeerJunior {
		t.Fatalf("caller observation = %+v/%s", roster.caller, roster.classification)
	}
}
