package services

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestRegistryAbsorbsAndReportsByHash(t *testing.T) {
	reg := NewTrustedSeedRegistry(10)
	reg.Absorb(context.Background(), callerSeed("a", "203.0.113.1", 8090))
	reg.Absorb(context.Background(), callerSeed("a", "203.0.113.2", 8090))

	trusted := reg.Trusted(context.Background())
	if len(trusted) != 1 {
		t.Fatalf("got %d seeds, want 1 deduplicated by hash", len(trusted))
	}
	if trusted[0][yacymodel.SeedIP] != "203.0.113.2" {
		t.Errorf("got ip %q, want latest absorbed", trusted[0][yacymodel.SeedIP])
	}
}

func TestRegistryDiscardsSeedWithoutHash(t *testing.T) {
	reg := NewTrustedSeedRegistry(10)
	reg.Absorb(context.Background(), yacymodel.Seed{yacymodel.SeedIP: "203.0.113.1"})

	if got := len(reg.Trusted(context.Background())); got != 0 {
		t.Fatalf("got %d seeds, want 0", got)
	}
}

func TestRegistryHonorsCapacityForNewHashes(t *testing.T) {
	reg := NewTrustedSeedRegistry(1)
	reg.Absorb(context.Background(), callerSeed("a", "203.0.113.1", 8090))
	reg.Absorb(context.Background(), callerSeed("b", "203.0.113.2", 8090))

	if got := len(reg.Trusted(context.Background())); got != 1 {
		t.Fatalf("got %d seeds, want capacity of 1", got)
	}
}
