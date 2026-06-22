package services

import (
	"context"
	"testing"
)

func TestRegistryAbsorbsAndReportsByHash(t *testing.T) {
	reg := NewTrustedSeedRegistry(10)
	reg.Absorb(context.Background(), callerSeed(t, "a", "203.0.113.1", 8090))
	reg.Absorb(context.Background(), callerSeed(t, "a", "203.0.113.2", 8090))

	trusted := reg.Trusted(context.Background())
	if len(trusted) != 1 {
		t.Fatalf("got %d seeds, want 1 deduplicated by hash", len(trusted))
	}
	if ip, ok := trusted[0].IP.Get(); !ok || ip.String() != "203.0.113.2" {
		t.Errorf("got ip %q, want latest absorbed", ip)
	}
}

func TestRegistryHonorsCapacityForNewHashes(t *testing.T) {
	reg := NewTrustedSeedRegistry(1)
	reg.Absorb(context.Background(), callerSeed(t, "a", "203.0.113.1", 8090))
	reg.Absorb(context.Background(), callerSeed(t, "b", "203.0.113.2", 8090))

	if got := len(reg.Trusted(context.Background())); got != 1 {
		t.Fatalf("got %d seeds, want capacity of 1", got)
	}
}
