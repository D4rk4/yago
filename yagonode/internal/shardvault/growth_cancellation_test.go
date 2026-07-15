package shardvault

import (
	"context"
	"errors"
	"testing"
)

func TestGrowShardsStopsBeforeSplitWhenCancellationFollowsMeasurement(t *testing.T) {
	saved := shardBytesTarget
	shardBytesTarget = 1
	t.Cleanup(func() { shardBytesTarget = saved })

	shards := openTestEngine(t)
	writeRecords(t, shards, 50)
	initialShards := len(shards.shards)
	ctx := &cancellationRaceContext{
		Context:  context.Background(),
		cancelAt: minShards + 3,
	}

	splits, err := shards.GrowShards(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if splits != 0 || len(shards.shards) != initialShards {
		t.Fatalf(
			"splits = %d and shards = %d, want 0 and %d",
			splits,
			len(shards.shards),
			initialShards,
		)
	}
}
