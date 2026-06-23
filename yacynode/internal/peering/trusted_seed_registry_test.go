package peering

import (
	"context"
	"testing"
)

func TestRegistryDiscardsBeyondCapacity(t *testing.T) {
	registry := NewTrustedSeeds(1)
	registry.Absorb(context.Background(), callerSeed(t, "a", "", 0))
	registry.Absorb(context.Background(), callerSeed(t, "b", "", 0))

	trusted := registry.Trusted(context.Background())
	if len(trusted) != 1 {
		t.Fatalf("trusted = %d, want 1 (capacity enforced)", len(trusted))
	}
	if trusted[0].Hash != hashFor("a") {
		t.Fatalf("retained %q, want first absorbed", trusted[0].Hash)
	}
}
