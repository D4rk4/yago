package shardvault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOnlineFreelistCheckpointPersistsCurrentAndLaterWrites(t *testing.T) {
	for _, deferredFsync := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred-fsync-%t", deferredFsync), func(t *testing.T) {
			assertOnlineFreelistCheckpointPersistence(t, deferredFsync)
		})
	}
}

func assertOnlineFreelistCheckpointPersistence(t *testing.T, deferredFsync bool) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "vault")
	engine, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	shardsClosed := false
	t.Cleanup(func() {
		if !shardsClosed {
			closeShards(engine.shards)
		}
	})
	if err := engine.Provision(testBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}
	engine.SetDeferredFsync(deferredFsync)
	if err := populateFreelistCheckpointEngine(t.Context(), engine); err != nil {
		t.Fatalf("populate engine: %v", err)
	}
	result, err := engine.CheckpointFreelists(t.Context())
	if err != nil || result.ShardsCheckpointed != minShards || result.ShardsTotal != minShards {
		t.Fatalf("checkpoint result=%+v error=%v", result, err)
	}
	assertFreelistCheckpointFlags(t, engine, deferredFsync)
	laterValue := []byte("after online checkpoint")
	if err := writeAfterFreelistCheckpoint(t.Context(), engine, laterValue); err != nil {
		t.Fatalf("write after checkpoint: %v", err)
	}
	if deferredFsync {
		if err := engine.SyncShards(t.Context()); err != nil {
			t.Fatalf("sync deferred writes: %v", err)
		}
	}
	closeShards(engine.shards)
	shardsClosed = true
	assertFreelistReopensWithoutRecoveryWrite(t, directory)
	assertFreelistCheckpointReopenedValue(t, directory, laterValue)
}

func populateFreelistCheckpointEngine(ctx context.Context, engine *engine) error {
	return engine.Update(ctx, func(transaction vault.EngineTxn) error {
		bucket := transaction.Bucket(testBucket)
		for index := range 128 {
			key := vault.Key(fmt.Sprintf("before-%03d", index))
			if err := bucket.Put(key, bytes.Repeat([]byte("v"), 128)); err != nil {
				return fmt.Errorf("store initial record: %w", err)
			}
		}
		for index := range 64 {
			if err := bucket.Delete(vault.Key(fmt.Sprintf("before-%03d", index))); err != nil {
				return fmt.Errorf("delete initial record: %w", err)
			}
		}

		return nil
	})
}

func assertFreelistCheckpointFlags(t *testing.T, engine *engine, deferredFsync bool) {
	t.Helper()
	for shard, database := range engine.shards {
		if database.NoFreelistSync {
			t.Fatalf("shard %d did not retain freelist persistence", shard)
		}
		if database.NoSync != deferredFsync {
			t.Fatalf("shard %d NoSync=%t, want %t", shard, database.NoSync, deferredFsync)
		}
	}
}

func writeAfterFreelistCheckpoint(
	ctx context.Context,
	engine *engine,
	value []byte,
) error {
	return engine.Update(ctx, func(transaction vault.EngineTxn) error {
		return transaction.Bucket(testBucket).Put(vault.Key("after"), value)
	})
}

func assertFreelistCheckpointReopenedValue(t *testing.T, directory string, value []byte) {
	t.Helper()
	reopened, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.View(t.Context(), func(transaction vault.EngineTxn) error {
		if got := transaction.Bucket(testBucket).Get(vault.Key("after")); !bytes.Equal(got, value) {
			t.Fatalf("later value = %q, want %q", got, value)
		}

		return nil
	}); err != nil {
		t.Fatalf("read later value: %v", err)
	}
}

func TestOnlineFreelistCheckpointWaitsForAndPausesVaultOperations(t *testing.T) {
	engine, err := openEngine(filepath.Join(t.TempDir(), "vault"), 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if err := engine.Provision(testBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}

	previousCheckpoint := checkpointLiveShardFreelist
	checkpointEntered := make(chan struct{})
	releaseCheckpoint := make(chan struct{})
	checkpointLiveShardFreelist = func(database *bolt.DB) error {
		select {
		case <-checkpointEntered:
		default:
			close(checkpointEntered)
		}
		<-releaseCheckpoint

		return previousCheckpoint(database)
	}
	t.Cleanup(func() { checkpointLiveShardFreelist = previousCheckpoint })

	readEntered := make(chan struct{})
	releaseRead := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		readDone <- engine.View(t.Context(), func(vault.EngineTxn) error {
			close(readEntered)
			<-releaseRead

			return nil
		})
	}()
	<-readEntered

	checkpointDone := make(chan error, 1)
	go func() {
		_, checkpointErr := engine.CheckpointFreelists(t.Context())
		checkpointDone <- checkpointErr
	}()
	waitForGateWriter(t, engine)
	select {
	case <-checkpointDone:
		t.Fatal("checkpoint passed an active vault read")
	default:
	}
	close(releaseRead)
	if err := <-readDone; err != nil {
		t.Fatalf("active vault read: %v", err)
	}
	select {
	case <-checkpointEntered:
	case <-time.After(time.Second):
		t.Fatal("checkpoint did not begin after the active read ended")
	}

	newReadDone := make(chan error, 1)
	go func() { newReadDone <- engine.View(t.Context(), func(vault.EngineTxn) error { return nil }) }()
	select {
	case err := <-newReadDone:
		t.Fatalf("new vault read passed active checkpoint: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseCheckpoint)
	if err := <-checkpointDone; err != nil {
		t.Fatalf("online checkpoint: %v", err)
	}
	if err := <-newReadDone; err != nil {
		t.Fatalf("vault read after checkpoint: %v", err)
	}
}

func TestOnlineFreelistCheckpointCancelledBeforeQuiescenceDoesNotWrite(t *testing.T) {
	engine, err := openEngine(filepath.Join(t.TempDir(), "vault"), 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	before := shardTransactionIdentity(t, engine.shards[0])

	engine.globalGate.RLock()
	ctx, cancel := context.WithCancel(t.Context())
	checkpointDone := make(chan error, 1)
	go func() {
		_, checkpointErr := engine.CheckpointFreelists(ctx)
		checkpointDone <- checkpointErr
	}()
	waitForGateWriter(t, engine)
	cancel()
	engine.globalGate.RUnlock()
	if err := <-checkpointDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint error = %v, want cancellation", err)
	}
	if after := shardTransactionIdentity(t, engine.shards[0]); after != before {
		t.Fatalf("cancelled checkpoint changed transaction identity from %d to %d", before, after)
	}
	if !engine.shards[0].NoFreelistSync {
		t.Fatal("cancelled checkpoint changed freelist persistence")
	}
}

func TestOnlineFreelistCheckpointAttemptsEveryShardAndReportsFailures(t *testing.T) {
	databases := openCheckpointTestShards(t, 3)
	checkpointFailure := errors.New("checkpoint failure")
	syncFailure := errors.New("sync failure")
	checkpointed := make([]int, 0, len(databases))
	synced := make([]int, 0, len(databases))
	result := vault.FreelistCheckpointResult{ShardsTotal: len(databases)}
	err := checkpointOpenShardFreelists(
		databases,
		true,
		freelistCheckpointOperations{
			checkpoint: func(database *bolt.DB) error {
				index := indexOfCheckpointDatabase(databases, database)
				checkpointed = append(checkpointed, index)
				if index == 1 {
					return checkpointFailure
				}

				return nil
			},
			sync: func(database *bolt.DB) error {
				index := indexOfCheckpointDatabase(databases, database)
				synced = append(synced, index)

				return syncFailure
			},
		},
		&result,
	)
	if !errors.Is(err, checkpointFailure) || !errors.Is(err, syncFailure) ||
		!strings.Contains(err.Error(), "checkpoint shard 2") ||
		!strings.Contains(err.Error(), "sync shard 2") {
		t.Fatalf("checkpoint error = %v, want shard failures", err)
	}
	if result.ShardsCheckpointed != 2 || result.ShardsTotal != 3 ||
		fmt.Sprint(checkpointed) != "[0 1 2]" || fmt.Sprint(synced) != "[1]" {
		t.Fatalf("result=%+v checkpointed=%v synced=%v", result, checkpointed, synced)
	}
	for index, database := range databases {
		if database.NoFreelistSync || !database.NoSync {
			t.Fatalf(
				"shard %d flags NoFreelistSync=%t NoSync=%t",
				index,
				database.NoFreelistSync,
				database.NoSync,
			)
		}
	}
}

func openCheckpointTestShards(t *testing.T, count int) []*bolt.DB {
	t.Helper()
	databases := make([]*bolt.DB, 0, count)
	for index := range count {
		database, err := bolt.Open(
			filepath.Join(t.TempDir(), fmt.Sprintf("shard-%d.vlt", index)),
			0o600,
			openTimeoutOptions(),
		)
		if err != nil {
			t.Fatalf("open shard %d: %v", index, err)
		}
		database.NoSync = true
		databases = append(databases, database)
	}
	t.Cleanup(func() {
		for _, database := range databases {
			_ = database.Close()
		}
	})

	return databases
}

func indexOfCheckpointDatabase(databases []*bolt.DB, candidate *bolt.DB) int {
	for index, database := range databases {
		if database == candidate {
			return index
		}
	}

	return -1
}

func waitForGateWriter(t *testing.T, engine *engine) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !engine.globalGate.TryRLock() {
			return
		}
		engine.globalGate.RUnlock()
		runtime.Gosched()
	}
	t.Fatal("checkpoint did not queue for the exclusive vault gate")
}
