package yagonode

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/shardvault"
)

func TestFreelistCheckpointSourceRequiresPersistentShardVault(t *testing.T) {
	if newFreelistCheckpointSource(nil) != nil {
		t.Fatal("nil vault exposed the checkpoint action")
	}

	inMemory, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = inMemory.Close() })
	if newFreelistCheckpointSource(inMemory) != nil {
		t.Fatal("in-memory vault exposed a persistent freelist action")
	}

	persistent, err := shardvault.Open(filepath.Join(t.TempDir(), "vault"), 1<<20)
	if err != nil {
		t.Fatalf("shardvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = persistent.Close() })
	source := newFreelistCheckpointSource(persistent)
	if source == nil {
		t.Fatal("persistent shard vault did not expose the checkpoint action")
	}
	result, err := source.CheckpointFreelists(context.Background())
	if err != nil || result.ShardsCheckpointed != 8 || result.ShardsTotal != 8 {
		t.Fatalf("checkpoint result=%+v error=%v", result, err)
	}
}
