package shardvault

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func swapCompactMinBytes(v int64) func() {
	prev := compactMinBytes
	compactMinBytes = v

	return func() { compactMinBytes = prev }
}

// populateThenPrune fills the store with `total` incompressible records, deletes
// all but the first `keep`, and returns the collection so the caller can verify
// the survivors. It leaves the shard files at their high-water size with most
// pages freed — the state compaction reclaims.
func populateThenPrune(
	t *testing.T,
	vaulted *vault.Vault,
	total, keep int,
) *vault.Collection[string] {
	t.Helper()
	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	if err := vaulted.Update(ctx, func(txn *vault.Txn) error {
		for i := range total {
			value := incompressibleValue(uint64(i), 1024)
			if err := values.Put(txn, vault.Key(fmt.Sprintf("doc-%05d", i)), value); err != nil {
				return fmt.Errorf("put: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("populate: %v", err)
	}
	if err := vaulted.Update(ctx, func(txn *vault.Txn) error {
		for i := keep; i < total; i++ {
			if _, err := values.Delete(txn, vault.Key(fmt.Sprintf("doc-%05d", i))); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("prune: %v", err)
	}

	return values
}

func TestCompactReclaimsFreedPagesAndKeepsData(t *testing.T) {
	defer swapCompactMinBytes(64 << 10)() // 64 KiB floor so a modest dataset qualifies
	dir := filepath.Join(t.TempDir(), "vault")
	vaulted, err := Open(dir, 64<<20)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })
	ctx := context.Background()

	const total, keep = 6000, 200
	values := populateThenPrune(t, vaulted, total, keep)

	fileBefore := totalShardFileBytes(dir)
	result, err := vaulted.Compact(ctx)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if result.ShardsCompacted == 0 || result.BytesReclaimed <= 0 {
		t.Fatalf("compaction reclaimed nothing: %+v", result)
	}
	if fileAfter := totalShardFileBytes(dir); fileAfter >= fileBefore {
		t.Fatalf("shard files did not shrink: before=%d after=%d", fileBefore, fileAfter)
	}

	// The surviving records are intact after the rewrite.
	if err := vaulted.View(ctx, func(txn *vault.Txn) error {
		for i := range keep {
			got, ok, err := values.Get(txn, vault.Key(fmt.Sprintf("doc-%05d", i)))
			if err != nil || !ok {
				return fmt.Errorf("missing doc-%05d ok=%v err=%w", i, ok, err)
			}
			if got != incompressibleValue(uint64(i), 1024) {
				return fmt.Errorf("doc-%05d corrupted after compaction", i)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("verify survivors: %v", err)
	}
}

func TestCompactSkipsHealthyShards(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	vaulted, err := Open(dir, 64<<20)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })
	ctx := context.Background()

	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := vaulted.Update(ctx, func(txn *vault.Txn) error {
		return values.Put(txn, vault.Key("k"), "v")
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	// With the production multi-megabyte floor, a tiny store compacts nothing.
	result, err := vaulted.Compact(ctx)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if result.ShardsCompacted != 0 || result.BytesReclaimed != 0 {
		t.Fatalf("a healthy store must skip compaction, got %+v", result)
	}
}

// TestCompactIsSafeUnderConcurrentReads runs a compaction while readers hammer
// the store, exercising the shared/exclusive gate. It must not deadlock (test
// timeout) or race (go test -race) on the shard-handle swap, and the survivors
// stay readable throughout.
func TestCompactIsSafeUnderConcurrentReads(t *testing.T) {
	defer swapCompactMinBytes(64 << 10)()
	dir := filepath.Join(t.TempDir(), "vault")
	vaulted, err := Open(dir, 64<<20)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })
	ctx := context.Background()

	values := populateThenPrune(t, vaulted, 4000, 100)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = vaulted.View(ctx, func(txn *vault.Txn) error {
					_, _, _ = values.Get(txn, vault.Key("doc-00000"))

					return nil
				})
				_, _ = vaulted.UsedBytes(ctx)
			}
		}()
	}

	if _, err := vaulted.Compact(ctx); err != nil {
		close(stop)
		wg.Wait()
		t.Fatalf("compact under reads: %v", err)
	}
	close(stop)
	wg.Wait()

	if err := vaulted.View(ctx, func(txn *vault.Txn) error {
		if _, ok, err := values.Get(txn, vault.Key("doc-00000")); err != nil || !ok {
			return fmt.Errorf("survivor missing after compaction ok=%v err=%w", ok, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
