package yagonode

import (
	"slices"
	"testing"
)

func TestPrioritizedSwarmMorphologyFormsReservesGeneratedAndObservedSlots(t *testing.T) {
	generated := []string{
		"source", "generated-1", "generated-2", "generated-3", "generated-4",
		"generated-5", "generated-6", "generated-7", "generated-8", "generated-9",
	}
	observed := []string{"source", "observed-1", "observed-2", "observed-3", "observed-4"}
	got := prioritizedSwarmMorphologyForms(observed, generated)
	if !slices.Contains(got[:6], "generated-3") ||
		!slices.Contains(got[:6], "observed-1") ||
		!slices.Contains(got[:12], "generated-8") ||
		!slices.Contains(got[:12], "observed-3") {
		t.Fatalf("prioritized forms = %v", got)
	}
	if got := prioritizedSwarmMorphologyForms(nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("empty prioritized forms = %#v", got)
	}
	if got := prioritizedSwarmMorphologyForms(
		[]string{"observed-only"},
		nil,
	); !slices.Equal(got, []string{"observed-only"}) {
		t.Fatalf("observed-only prioritized forms = %#v", got)
	}
}
