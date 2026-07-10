package shardvault

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// openEngineForClose builds an engine the test closes itself, so the Close paths
// can be exercised without the auto-close cleanup openTestEngine registers.
func openEngineForClose(t *testing.T) *engine {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "vault")
	e, err := openEngine(dir, 1<<20)
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}

	return e
}

func TestSetDeferredFsyncTogglesShardsAndFlag(t *testing.T) {
	e := openTestEngine(t)
	if e.DeferredFsyncEnabled() {
		t.Fatal("fresh engine already deferring fsync")
	}

	e.SetDeferredFsync(true)
	if !e.DeferredFsyncEnabled() {
		t.Fatal("SetDeferredFsync(true) did not enable the mode")
	}
	for i, db := range e.shards {
		if !db.NoSync {
			t.Fatalf("shard %d NoSync=false after enabling deferred fsync", i)
		}
	}

	e.SetDeferredFsync(false)
	if e.DeferredFsyncEnabled() {
		t.Fatal("SetDeferredFsync(false) did not disable the mode")
	}
	for i, db := range e.shards {
		if db.NoSync {
			t.Fatalf("shard %d NoSync=true after disabling deferred fsync", i)
		}
	}
}

func TestSyncShardsFlushesEveryShardStaggered(t *testing.T) {
	e := openTestEngine(t)

	var synced int
	restoreSync := syncDB
	syncDB = func(*bolt.DB) error { synced++; return nil }
	t.Cleanup(func() { syncDB = restoreSync })

	var pauses int
	restorePause := pauseBetweenShardSyncs
	pauseBetweenShardSyncs = func(context.Context, time.Duration) error { pauses++; return nil }
	t.Cleanup(func() { pauseBetweenShardSyncs = restorePause })

	if err := e.SyncShards(context.Background()); err != nil {
		t.Fatalf("SyncShards: %v", err)
	}
	if synced != len(e.shards) {
		t.Fatalf("synced %d shards, want %d", synced, len(e.shards))
	}
	if pauses != len(e.shards)-1 {
		t.Fatalf("paused %d times, want %d", pauses, len(e.shards)-1)
	}
}

func TestSyncShardsEmptyPoolIsNoop(t *testing.T) {
	e := &engine{}
	if err := e.SyncShards(context.Background()); err != nil {
		t.Fatalf("SyncShards on empty pool: %v", err)
	}
}

func TestSyncShardsReportsFlushError(t *testing.T) {
	e := openTestEngine(t)

	restoreSync := syncDB
	syncDB = func(*bolt.DB) error { return errors.New("flush failed") }
	t.Cleanup(func() { syncDB = restoreSync })

	if err := e.SyncShards(context.Background()); err == nil {
		t.Fatal("SyncShards ignored a shard flush error")
	}
}

func TestSyncShardsReportsPauseError(t *testing.T) {
	e := openTestEngine(t)

	restoreSync := syncDB
	syncDB = func(*bolt.DB) error { return nil }
	t.Cleanup(func() { syncDB = restoreSync })

	pauseErr := errors.New("interrupted")
	restorePause := pauseBetweenShardSyncs
	pauseBetweenShardSyncs = func(context.Context, time.Duration) error { return pauseErr }
	t.Cleanup(func() { pauseBetweenShardSyncs = restorePause })

	if err := e.SyncShards(context.Background()); !errors.Is(err, pauseErr) {
		t.Fatalf("SyncShards error = %v, want %v", err, pauseErr)
	}
}

func TestSleepWithContext(t *testing.T) {
	if err := sleepWithContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleepWithContext elapsed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepWithContext(ctx, time.Hour); err == nil {
		t.Fatal("sleepWithContext ignored a cancelled context")
	}
}

func TestCloseFlushesShardsWhenDeferred(t *testing.T) {
	e := openEngineForClose(t)
	e.SetDeferredFsync(true)

	var synced int
	restoreSync := syncDB
	syncDB = func(*bolt.DB) error { synced++; return nil }
	t.Cleanup(func() { syncDB = restoreSync })

	shards := len(e.shards)
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if synced != shards {
		t.Fatalf("Close flushed %d shards, want %d", synced, shards)
	}
}

func TestCloseSkipsFlushWhenNotDeferred(t *testing.T) {
	e := openEngineForClose(t)

	var synced int
	restoreSync := syncDB
	syncDB = func(*bolt.DB) error { synced++; return nil }
	t.Cleanup(func() { syncDB = restoreSync })

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if synced != 0 {
		t.Fatalf("Close flushed %d shards with deferred fsync off, want 0", synced)
	}
}

func TestCloseReportsFlushErrorWhenDeferred(t *testing.T) {
	e := openEngineForClose(t)
	e.SetDeferredFsync(true)

	restoreSync := syncDB
	syncDB = func(*bolt.DB) error { return errors.New("flush failed") }
	t.Cleanup(func() { syncDB = restoreSync })

	if err := e.Close(); err == nil {
		t.Fatal("Close ignored a shard flush error under deferred fsync")
	}
}
