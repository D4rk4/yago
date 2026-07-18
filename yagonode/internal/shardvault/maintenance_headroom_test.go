package shardvault

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func TestMaintenanceHeadroomMatchesCopySources(t *testing.T) {
	t.Run("compaction", func(t *testing.T) {
		defer swapCompactMinBytes(64 << 10)()
		vaulted, err := Open(filepath.Join(t.TempDir(), "vault"), 64<<20)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = vaulted.Close() })
		populateThenPrune(t, vaulted, 6000, 200)
		required, err := vaulted.CompactionHeadroom(t.Context())
		if err != nil || required == 0 {
			t.Fatalf("compaction headroom=%d error=%v", required, err)
		}
	})

	t.Run("split", func(t *testing.T) {
		saved := shardBytesTarget
		shardBytesTarget = 4 << 10
		t.Cleanup(func() { shardBytesTarget = saved })
		engine := openTestEngine(t)
		writeRecords(t, engine, 4000)
		required, err := engine.ShardGrowthHeadroom(t.Context())
		if err != nil {
			t.Fatalf("growth headroom: %v", err)
		}
		size, _, err := shardSizeAndFree(engine.shards[engine.split])
		if err != nil || required != nonNegativeHeadroom(size) {
			t.Fatalf("growth headroom=%d source=%d error=%v", required, size, err)
		}
		shardBytesTarget = 1 << 40
		if required, err = engine.ShardGrowthHeadroom(t.Context()); err != nil || required != 0 {
			t.Fatalf("under-target headroom=%d error=%v", required, err)
		}
	})
}

func TestMaintenanceHeadroomRejectsCancelledAndClosedSources(t *testing.T) {
	engine := openTestEngine(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := engine.CompactionHeadroom(ctx); err == nil {
		t.Fatal("cancelled compaction headroom succeeded")
	}
	if _, err := engine.ShardGrowthHeadroom(ctx); err == nil {
		t.Fatal("cancelled growth headroom succeeded")
	}
	if err := engine.shards[0].Close(); err != nil {
		t.Fatalf("close source shard: %v", err)
	}
	if _, err := engine.CompactionHeadroom(t.Context()); err == nil {
		t.Fatal("closed compaction source measured")
	}
	if _, err := engine.ShardGrowthHeadroom(t.Context()); err == nil {
		t.Fatal("closed growth source measured")
	}
}

func TestMaintenanceHeadroomArithmeticSaturates(t *testing.T) {
	if got := saturatingShardBytes(math.MaxInt64-1, 2); got != math.MaxInt64 {
		t.Fatalf("saturated shard bytes = %d, want %d", got, int64(math.MaxInt64))
	}
	if got := saturatingShardBytes(4, 3); got != 7 {
		t.Fatalf("added shard bytes = %d, want 7", got)
	}
	saved := shardBytesTarget
	shardBytesTarget = math.MaxInt64
	t.Cleanup(func() { shardBytesTarget = saved })
	if got := shardStorageTarget(8); got != math.MaxInt64 {
		t.Fatalf("saturated shard target = %d, want %d", got, int64(math.MaxInt64))
	}
	if got := nonNegativeHeadroom(0); got != 0 {
		t.Fatalf("zero headroom = %d, want 0", got)
	}
	if got := nonNegativeHeadroom(-1); got != 0 {
		t.Fatalf("negative headroom = %d, want 0", got)
	}
}
