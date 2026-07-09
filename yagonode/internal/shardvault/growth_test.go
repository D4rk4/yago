package shardvault

import (
	"context"
	"testing"
)

// TestGrowShardsSplitsWhileOverTarget: with a tiny per-shard target, a modest
// write load pushes the pool over target and GrowShards splits up to its cap,
// keeping every record.
func TestGrowShardsSplitsWhileOverTarget(t *testing.T) {
	saved := shardBytesTarget
	shardBytesTarget = 4 << 10
	t.Cleanup(func() { shardBytesTarget = saved })

	e := openTestEngine(t)
	want := writeRecords(t, e, 4000)

	splits, err := e.GrowShards(context.Background(), 3)
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	if splits == 0 {
		t.Fatal("expected the pool to grow when over target")
	}
	if splits > 3 {
		t.Fatalf("grew %d shards, over the per-call cap of 3", splits)
	}
	if len(e.shards) != 8+splits {
		t.Fatalf("shards = %d, want %d", len(e.shards), 8+splits)
	}
	assertSameRecords(t, want, readAllRecords(t, e))
}

// TestGrowShardsNoopUnderTarget: at the default multi-GiB target a few records
// never trip growth.
func TestGrowShardsNoopUnderTarget(t *testing.T) {
	e := openTestEngine(t)
	writeRecords(t, e, 500)

	splits, err := e.GrowShards(context.Background(), 4)
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	if splits != 0 || len(e.shards) != 8 {
		t.Fatalf("under target: splits=%d shards=%d, want 0/8", splits, len(e.shards))
	}
}
