package shardvault

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func keysOnOneShard(e *engine, count int) []string {
	shard := e.route(testBucket, vault.Key("record-0"))
	keys := make([]string, 0, count)
	for index := 0; len(keys) < count; index++ {
		key := fmt.Sprintf("record-%d", index)
		if e.route(testBucket, vault.Key(key)) == shard {
			keys = append(keys, key)
		}
	}

	return keys
}

func putRecord(key string) func(vault.EngineTxn) error {
	return func(txn vault.EngineTxn) error {
		return txn.Bucket(testBucket).Put(vault.Key(key), []byte(key))
	}
}

func countShardCommits(t *testing.T) *atomic.Int64 {
	t.Helper()
	var commits atomic.Int64
	savedCommit := commitTx
	t.Cleanup(func() { commitTx = savedCommit })
	commitTx = func(tx *bolt.Tx) error {
		commits.Add(1)

		return savedCommit(tx)
	}

	return &commits
}

func holdContendedAdmission(t *testing.T, e *engine) {
	t.Helper()
	if err := e.writeAdmission.enterContended(t.Context()); err != nil {
		t.Fatalf("hold contended admission: %v", err)
	}
}

func waitForQueuedContendedWrites(t *testing.T, e *engine, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.contendedWrites.lock.Lock()
		queued := len(e.contendedWrites.writes)
		e.contendedWrites.lock.Unlock()
		if queued == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued contended writes = %d, want %d", queued, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func startContendedWrites(
	t *testing.T,
	e *engine,
	updates []func(vault.EngineTxn) error,
) []chan error {
	t.Helper()
	outcomes := make([]chan error, len(updates))
	for index, update := range updates {
		outcomes[index] = make(chan error, 1)
		go func() { outcomes[index] <- e.updateContended(t.Context(), update) }()
	}
	waitForQueuedContendedWrites(t, e, len(updates))

	return outcomes
}

func receiveOutcome(t *testing.T, outcome chan error) error {
	t.Helper()
	select {
	case err := <-outcome:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("contended write did not finish")

		return nil
	}
}

func putThen(key string, returned, latched error) func(vault.EngineTxn) error {
	return func(txn vault.EngineTxn) error {
		if err := putRecord(key)(txn); err != nil {
			return err
		}
		if latched != nil {
			txn.(*shardTxn).latchAccessError(latched)
		}

		return returned
	}
}

func recordsFor(keys ...string) map[string]string {
	records := make(map[string]string, len(keys))
	for _, key := range keys {
		records[key] = key
	}

	return records
}

func TestQueuedContendedWritesShareOneCommit(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, 8)
	commits := countShardCommits(t)
	holdContendedAdmission(t, e)
	updates := make([]func(vault.EngineTxn) error, 0, len(keys))
	for _, key := range keys {
		updates = append(updates, putRecord(key))
	}
	outcomes := startContendedWrites(t, e, updates)

	e.writeAdmission.leaveContended()

	for _, outcome := range outcomes {
		if err := receiveOutcome(t, outcome); err != nil {
			t.Fatalf("contended write: %v", err)
		}
	}
	if commits.Load() != 1 {
		t.Fatalf("shard commits = %d, want 1 for one write group", commits.Load())
	}
	assertSameRecords(t, recordsFor(keys...), readAllRecords(t, e))
}

func TestContendedWriteGroupStopsAtItsLimit(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, contendedGroupLimit+1)
	commits := countShardCommits(t)
	holdContendedAdmission(t, e)
	updates := make([]func(vault.EngineTxn) error, 0, len(keys))
	for _, key := range keys {
		updates = append(updates, putRecord(key))
	}
	outcomes := startContendedWrites(t, e, updates)

	e.writeAdmission.leaveContended()

	for _, outcome := range outcomes {
		if err := receiveOutcome(t, outcome); err != nil {
			t.Fatalf("contended write: %v", err)
		}
	}
	if commits.Load() != 2 {
		t.Fatalf("shard commits = %d, want 2 for a full group plus one", commits.Load())
	}
	assertSameRecords(t, recordsFor(keys...), readAllRecords(t, e))
}

func TestContendedWriteGroupIsolatesAFailingMember(t *testing.T) {
	refused := errors.New("update refused")
	accessFailure := errors.New("bucket unreadable")
	failures := map[string]struct{ returned, latched error }{
		"returned error":         {returned: refused},
		"swallowed access error": {latched: accessFailure},
	}
	for name, failing := range failures {
		t.Run(name, func(t *testing.T) {
			e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
			keys := keysOnOneShard(e, 3)
			holdContendedAdmission(t, e)
			outcomes := startContendedWrites(t, e, []func(vault.EngineTxn) error{
				putRecord(keys[0]),
				putThen(keys[1], failing.returned, failing.latched),
				putRecord(keys[2]),
			})

			e.writeAdmission.leaveContended()

			if err := receiveOutcome(t, outcomes[1]); !errors.Is(err, refused) &&
				!errors.Is(err, accessFailure) {
				t.Fatalf("failing member outcome = %v, want its own failure", err)
			}
			for _, index := range []int{0, 2} {
				if err := receiveOutcome(t, outcomes[index]); err != nil {
					t.Fatalf("member %d outcome = %v, want success", index, err)
				}
			}
			assertSameRecords(t, recordsFor(keys[0], keys[2]), readAllRecords(t, e))
		})
	}
}

func TestContendedWriteGroupRerunsAPanickingMemberInItsOwnCaller(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, 2)
	holdContendedAdmission(t, e)
	panicked := make(chan any, 1)
	outcomes := startContendedWrites(t, e, []func(vault.EngineTxn) error{
		putRecord(keys[0]),
		putRecord(keys[1]),
	})
	go func() {
		defer func() { panicked <- recover() }()
		_ = e.updateContended(t.Context(), func(vault.EngineTxn) error {
			panic("member update panicked")
		})
	}()
	waitForQueuedContendedWrites(t, e, 3)

	e.writeAdmission.leaveContended()

	select {
	case recovered := <-panicked:
		if recovered != "member update panicked" {
			t.Fatalf("panicking caller recovered %v", recovered)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("panicking member did not panic in its own caller")
	}
	for index, outcome := range outcomes {
		if err := receiveOutcome(t, outcome); err != nil {
			t.Fatalf("member %d outcome = %v, want success", index, err)
		}
	}
	assertSameRecords(t, recordsFor(keys...), readAllRecords(t, e))
}

func TestContendedWriteGroupIsolatesAStructuralRootFailure(t *testing.T) {
	healthy, err := bolt.Open(filepath.Join(t.TempDir(), "healthy.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open healthy shard: %v", err)
	}
	t.Cleanup(func() { _ = healthy.Close() })
	e := &engine{
		shards:     []*bolt.DB{openStructuralRootRecordShard(t), healthy},
		shardLocks: make([]sync.Mutex, 2),
	}
	holdContendedAdmission(t, e)
	outcomes := startContendedWrites(t, e, []func(vault.EngineTxn) error{
		func(txn vault.EngineTxn) error {
			_, err := txn.(*shardTxn).shard(0)

			return err
		},
		func(txn vault.EngineTxn) error {
			tx, err := txn.(*shardTxn).shard(1)
			if err != nil {
				return err
			}
			if _, err := tx.CreateBucketIfNotExists([]byte("kept")); err != nil {
				return fmt.Errorf("create kept bucket: %w", err)
			}

			return nil
		},
	})

	e.writeAdmission.leaveContended()

	if err := receiveOutcome(t, outcomes[0]); !errors.Is(err, errStructuralRootRecord) {
		t.Fatalf("corrupt member outcome = %v, want structural root failure", err)
	}
	if err := receiveOutcome(t, outcomes[1]); err != nil {
		t.Fatalf("healthy member outcome = %v, want success", err)
	}
	if err := healthy.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("kept")) == nil {
			return errors.New("healthy member write missing")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestContendedWriteGroupReportsAFailedCommitToEveryMember(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, 3)
	failure := errors.New("disk refused commit")
	savedCommit := commitTx
	t.Cleanup(func() { commitTx = savedCommit })
	commitTx = func(*bolt.Tx) error { return failure }
	holdContendedAdmission(t, e)
	updates := make([]func(vault.EngineTxn) error, 0, len(keys))
	for _, key := range keys {
		updates = append(updates, putRecord(key))
	}
	outcomes := startContendedWrites(t, e, updates)

	e.writeAdmission.leaveContended()

	for index, outcome := range outcomes {
		if err := receiveOutcome(t, outcome); !errors.Is(err, failure) {
			t.Fatalf("member %d outcome = %v, want commit failure", index, err)
		}
	}
	commitTx = savedCommit
	assertSameRecords(t, map[string]string{}, readAllRecords(t, e))
}

func TestContendedWriteCancelledWhileQueuedWritesNothing(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, 2)
	holdContendedAdmission(t, e)
	outcomes := startContendedWrites(t, e, []func(vault.EngineTxn) error{putRecord(keys[0])})
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 1)
	go func() { cancelled <- e.updateContended(ctx, putRecord(keys[1])) }()
	waitForQueuedContendedWrites(t, e, 2)

	cancel()
	if err := receiveOutcome(t, cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write outcome = %v, want cancellation", err)
	}
	waitForQueuedContendedWrites(t, e, 1)
	e.writeAdmission.leaveContended()

	if err := receiveOutcome(t, outcomes[0]); err != nil {
		t.Fatalf("queued write outcome = %v, want success", err)
	}
	assertSameRecords(t, recordsFor(keys[0]), readAllRecords(t, e))
}

func TestContendedWriteCancelledAfterItsGroupFormedReportsTheCommit(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, 2)
	holdContendedAdmission(t, e)
	ctx, cancel := context.WithCancel(t.Context())
	outcome := make(chan error, 1)
	go func() { outcome <- e.updateContended(ctx, putRecord(keys[0])) }()
	waitForQueuedContendedWrites(t, e, 1)
	group := e.contendedWrites.take()

	cancel()
	e.commitContendedGroup(group)
	e.writeAdmission.leaveContended()

	if err := receiveOutcome(t, outcome); err != nil {
		t.Fatalf("taken write outcome = %v, want the committed result", err)
	}
	assertSameRecords(t, recordsFor(keys[0]), readAllRecords(t, e))
}

func TestContendedWriteRerunHonorsCancellation(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	keys := keysOnOneShard(e, 2)
	refused := errors.New("update refused")
	holdContendedAdmission(t, e)
	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 1)
	go func() {
		cancelled <- e.updateContended(ctx, func(txn vault.EngineTxn) error {
			cancel()

			return putRecord(keys[1])(txn)
		})
	}()
	waitForQueuedContendedWrites(t, e, 1)
	failing := make(chan error, 1)
	go func() {
		failing <- e.updateContended(t.Context(), func(vault.EngineTxn) error { return refused })
	}()
	waitForQueuedContendedWrites(t, e, 2)

	e.writeAdmission.leaveContended()

	if err := receiveOutcome(t, cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("rerun outcome = %v, want cancellation", err)
	}
	if err := receiveOutcome(t, failing); !errors.Is(err, refused) {
		t.Fatalf("failing member outcome = %v, want refusal", err)
	}
	assertSameRecords(t, map[string]string{}, readAllRecords(t, e))
}
