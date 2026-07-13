package yagonode

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestAdminSearchFailuresHideInternalWebProviderName(t *testing.T) {
	got := adminSearchFailures([]searchcore.PartialFailure{
		{Source: searchcore.PartialFailureSourceWeb, Reason: "provider failed"},
		{Source: searchcore.PartialFailureSourceRemoteYaCy, Reason: "peer timed out"},
	})
	want := []string{"web: provider failed", "remote-yacy: peer timed out"}
	if !slices.Equal(got, want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
}
