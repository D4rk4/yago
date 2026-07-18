package shardvault

import (
	"context"
	"fmt"
	"math"
)

func (e *engine) CompactionHeadroom(ctx context.Context) (uint64, error) {
	if err := acquireGlobalRead(ctx, &e.globalGate); err != nil {
		return 0, err
	}
	defer e.globalGate.RUnlock()
	var largest int64
	for shard, database := range e.shards {
		size, free, err := shardSizeAndFree(database)
		if err != nil {
			return 0, fmt.Errorf("measure compactable shard %d: %w", shard, err)
		}
		if worthCompacting(size, free) && size > largest {
			largest = size
		}
	}

	return nonNegativeHeadroom(largest), nil
}

func (e *engine) ShardGrowthHeadroom(ctx context.Context) (uint64, error) {
	if err := acquireGlobalRead(ctx, &e.globalGate); err != nil {
		return 0, err
	}
	defer e.globalGate.RUnlock()
	var used int64
	var splitSize int64
	for shard, database := range e.shards {
		size, free, err := shardSizeAndFree(database)
		if err != nil {
			return 0, fmt.Errorf("measure shard %d growth headroom: %w", shard, err)
		}
		live := max(size-free, 0)
		used = saturatingShardBytes(used, live)
		if shard == e.split {
			splitSize = size
		}
	}
	target := shardStorageTarget(len(e.shards))
	if used <= target {
		return 0, nil
	}

	return nonNegativeHeadroom(splitSize), nil
}

func saturatingShardBytes(current, additional int64) int64 {
	if additional > math.MaxInt64-current {
		return math.MaxInt64
	}

	return current + additional
}

func shardStorageTarget(shards int) int64 {
	if shardBytesTarget > math.MaxInt64/int64(shards) {
		return math.MaxInt64
	}

	return int64(shards) * shardBytesTarget
}

func nonNegativeHeadroom(bytes int64) uint64 {
	if bytes <= 0 {
		return 0
	}

	return uint64(bytes)
}
