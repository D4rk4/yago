package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPeerSearchFailureTotalCountsUniqueValidatedPeerHashes(t *testing.T) {
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
		{Source: "AAAAAAAAAAAA"},
	}
	if got := peerSearchFailureTotal(failures); got != 1 {
		t.Fatalf("peer failure total = %d, want 1", got)
	}
	if !federationSearchUnavailable(failures) {
		t.Fatal("aggregate federation failures must be reported without inventing peers")
	}
	if federationSearchUnavailable([]searchcore.PartialFailure{
		{Source: "AAAAAAAAAAAA"},
		{Source: searchcore.PartialFailureSourceWeb},
	}) {
		t.Fatal("identified peer and web failures are not aggregate federation failures")
	}
}
