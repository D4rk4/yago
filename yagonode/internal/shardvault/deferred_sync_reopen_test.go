package shardvault

import (
	"context"
	"fmt"
	"testing"
)

func TestCompactedShardKeepsDeferredFsyncMode(t *testing.T) {
	for _, deferred := range []bool{true, false} {
		t.Run(fmt.Sprintf("deferred=%v", deferred), func(t *testing.T) {
			e := churnedEngine(t)
			defer swapCompactMinBytes(8 << 10)()
			e.SetDeferredFsync(deferred)
			index := firstCompactableShard(t, e)

			_, compacted, err := e.compactShard(index)
			if err != nil || !compacted {
				t.Fatalf("compactShard = %v, %v", compacted, err)
			}
			if e.shards[index].NoSync != deferred {
				t.Fatalf("compacted shard NoSync=%v", e.shards[index].NoSync)
			}
		})
	}
}

func TestSplitShardKeepsDeferredFsyncMode(t *testing.T) {
	for _, deferred := range []bool{true, false} {
		t.Run(fmt.Sprintf("deferred=%v", deferred), func(t *testing.T) {
			e := openTestEngine(t)
			writeRecords(t, e, 3000)
			e.SetDeferredFsync(deferred)

			grew, err := e.SplitStep(context.Background())
			if err != nil || !grew {
				t.Fatalf("SplitStep = %v, %v", grew, err)
			}
			if opened := e.shards[len(e.shards)-1]; opened.NoSync != deferred {
				t.Fatalf("split shard NoSync=%v", opened.NoSync)
			}
		})
	}
}
