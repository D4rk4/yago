package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPeerSearchFailureTotalExcludesLocalAndWebStages(t *testing.T) {
	failures := []searchcore.PartialFailure{
		{Source: searchcore.PartialFailureSourceRemoteYaCy},
		{Source: searchcore.PartialFailureSourcePeerReputation},
		{Source: "AAAAAAAAAAAA"},
		{Source: searchcore.PartialFailureSourceExactStage},
		{Source: searchcore.PartialFailureSourceLocalExactStage},
		{Source: searchcore.PartialFailureSourceFuzzyStage},
		{Source: searchcore.PartialFailureSourceLocalSearch},
		{Source: searchcore.PartialFailureSourceWeb},
		{Source: "not-a-peer"},
	}
	if got := peerSearchFailureTotal(failures); got != 3 {
		t.Fatalf("peer failure total = %d, want 3", got)
	}
}
