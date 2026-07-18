package shardvault

import (
	"context"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestBucketPresenceDistinguishesAbsentCompleteAndPartialProvisioning(t *testing.T) {
	engine := openTestEngine(t)
	present, err := engine.BucketProvisioned(t.Context(), testBucket)
	if err != nil || !present {
		t.Fatalf("complete bucket presence=%t error=%v", present, err)
	}
	present, err = engine.BucketProvisioned(t.Context(), vault.Name("absent"))
	if err != nil || present {
		t.Fatalf("absent bucket presence=%t error=%v", present, err)
	}
	if err := engine.shards[0].Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket([]byte(testBucket))
	}); err != nil {
		t.Fatalf("remove bucket from one shard: %v", err)
	}
	present, err = engine.BucketProvisioned(t.Context(), testBucket)
	if err == nil || present || !strings.Contains(err.Error(), "present on 7 of 8 shards") {
		t.Fatalf("partial bucket presence=%t error=%v", present, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := engine.BucketProvisioned(ctx, testBucket); err == nil {
		t.Fatal("cancelled bucket inspection succeeded")
	}
	if err := engine.shards[0].Close(); err != nil {
		t.Fatalf("close source shard: %v", err)
	}
	if _, err := engine.BucketProvisioned(t.Context(), testBucket); err == nil ||
		!strings.Contains(err.Error(), "shard 0") {
		t.Fatalf("closed shard bucket inspection error = %v", err)
	}
}
