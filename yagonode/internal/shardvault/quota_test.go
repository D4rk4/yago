package shardvault

import (
	"context"
	"testing"
)

// TestSetQuotaBytesLiveCeiling: the engine's quota is a mutable ceiling — a
// SetQuotaBytes is visible to the next QuotaBytes without reopening the vault
// (ADR-0037 D, the #5 fix).
func TestSetQuotaBytesLiveCeiling(t *testing.T) {
	e := openTestEngine(t)
	if got := e.QuotaBytes(); got != 1<<20 {
		t.Fatalf("initial quota = %d, want %d", got, int64(1)<<20)
	}

	e.SetQuotaBytes(768 << 30)
	if got := e.QuotaBytes(); got != 768<<30 {
		t.Fatalf("after SetQuotaBytes quota = %d, want %d", got, int64(768)<<30)
	}
}

// TestGrowShardsPreservesQuota: growing the pool redistributes records across
// more shards but must leave the live quota ceiling untouched.
func TestGrowShardsPreservesQuota(t *testing.T) {
	saved := shardBytesTarget
	shardBytesTarget = 4 << 10
	t.Cleanup(func() { shardBytesTarget = saved })

	e := openTestEngine(t)
	e.SetQuotaBytes(500 << 30)
	writeRecords(t, e, 4000)

	if _, err := e.GrowShards(context.Background(), 3); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if got := e.QuotaBytes(); got != 500<<30 {
		t.Fatalf("quota after growth = %d, want %d", got, int64(500)<<30)
	}
}
