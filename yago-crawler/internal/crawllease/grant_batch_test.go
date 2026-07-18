package crawllease

import "testing"

func TestGrantRegistryTracksReplayBatchAtomically(t *testing.T) {
	registry := NewGrantRegistry(t.Context(), 3)
	added, err := registry.TrackMany([]string{"lease-a", "lease-a", "lease-b"})
	if err != nil || len(added) != 2 || added[0] != "lease-a" || added[1] != "lease-b" {
		t.Fatalf("first replay batch added=%v error=%v", added, err)
	}
	added, err = registry.TrackMany([]string{"lease-b", "lease-c"})
	if err != nil || len(added) != 1 || added[0] != "lease-c" {
		t.Fatalf("overlapping replay batch added=%v error=%v", added, err)
	}
	if _, err := registry.TrackMany([]string{"lease-d"}); err == nil {
		t.Fatal("over-capacity replay batch succeeded")
	}
	if active := registry.ActiveLeaseIDs(); len(active) != 3 {
		t.Fatalf("capacity failure changed active grants: %v", active)
	}
	if _, err := registry.TrackMany([]string{""}); err == nil {
		t.Fatal("invalid replay lease succeeded")
	}
}
