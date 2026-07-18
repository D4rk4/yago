package crawllease

import (
	"testing"
	"time"
)

func TestGrantRegistrySignalsOnlyLeaseAvailabilityChanges(t *testing.T) {
	registry := NewGrantRegistry(t.Context(), 1)
	changes := registry.AvailabilityChanges()
	if err := registry.Track("availability-lease"); err != nil {
		t.Fatalf("track availability lease: %v", err)
	}
	awaitAvailabilityChange(t, changes)
	changes = registry.AvailabilityChanges()
	if err := registry.Track("availability-lease"); err != nil {
		t.Fatalf("repeat availability lease: %v", err)
	}
	rejectAvailabilityChange(t, changes)
	started := time.Now()
	registry.Renew(
		started,
		time.Hour,
		[]string{"availability-lease"},
		[]string{"availability-lease"},
	)
	awaitAvailabilityChange(t, changes)
	changes = registry.AvailabilityChanges()
	registry.Renew(
		started.Add(time.Second),
		time.Hour,
		[]string{"availability-lease"},
		[]string{"availability-lease"},
	)
	rejectAvailabilityChange(t, changes)
	registry.Revoke("availability-lease")
	awaitAvailabilityChange(t, changes)
}

func awaitAvailabilityChange(t *testing.T, changes <-chan struct{}) {
	t.Helper()
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("lease availability change was not signaled")
	}
}

func rejectAvailabilityChange(t *testing.T, changes <-chan struct{}) {
	t.Helper()
	select {
	case <-changes:
		t.Fatal("unchanged lease availability was signaled")
	case <-time.After(20 * time.Millisecond):
	}
}
