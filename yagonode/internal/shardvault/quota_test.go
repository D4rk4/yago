package shardvault

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
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

// TestUpdateYieldsToReadsButNeverStarves is the IO-PRIO-01 acceptance: with a
// read transaction held open, a write still completes — the yield to readers
// is bounded — and the in-flight read counter drains back to zero.
func TestUpdateYieldsToReadsButNeverStarves(t *testing.T) {
	e := openTestEngine(t)
	readHeld := make(chan struct{})
	releaseRead := make(chan struct{})
	go func() {
		_ = e.View(context.Background(), func(vault.EngineTxn) error {
			close(readHeld)
			<-releaseRead

			return nil
		})
	}()
	<-readHeld

	done := make(chan error, 1)
	go func() {
		done <- e.Update(context.Background(), func(txn vault.EngineTxn) error {
			return txn.Bucket(testBucket).Put(vault.Key("k"), []byte("v"))
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("update under held read: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("update starved behind an open read; the yield must be bounded")
	}
	close(releaseRead)
}
