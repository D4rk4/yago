package shardvault

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestUpdateCommitsWrittenShardsConcurrently(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	first, second := keysOnDistinctShards(e)
	arrivals := make(chan struct{}, 2)
	var meet sync.WaitGroup
	meet.Add(2)
	savedCommit := commitTx
	t.Cleanup(func() { commitTx = savedCommit })
	commitTx = func(tx *bolt.Tx) error {
		arrivals <- struct{}{}
		meet.Done()
		met := make(chan struct{})
		go func() {
			meet.Wait()
			close(met)
		}()
		select {
		case <-met:
		case <-time.After(5 * time.Second):
			return errors.New("shard commits ran one after another")
		}

		return savedCommit(tx)
	}

	putRecords(t, e, map[string]string{first: "first", second: "second"})

	if len(arrivals) != 2 {
		t.Fatalf("committed shards = %d, want 2", len(arrivals))
	}
	commitTx = savedCommit
	assertSameRecords(
		t,
		map[string]string{first: "first", second: "second"},
		readAllRecords(t, e),
	)
}

func TestUpdateReportsFailedShardCommitAndKeepsItsWritesOut(t *testing.T) {
	e := openPendingWritesEngine(t, filepath.Join(t.TempDir(), "vault"))
	kept, refused := keysOnDistinctShards(e)
	refusedShard := e.shards[e.route(testBucket, vault.Key(refused))]
	refusal := errors.New("shard refused commit")
	savedCommit := commitTx
	t.Cleanup(func() { commitTx = savedCommit })
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == refusedShard {
			return refusal
		}

		return savedCommit(tx)
	}

	err := e.Update(t.Context(), func(txn vault.EngineTxn) error {
		bucket := txn.Bucket(testBucket)
		if err := bucket.Put(vault.Key(kept), []byte("kept")); err != nil {
			return fmt.Errorf("put kept record: %w", err)
		}
		if err := bucket.Put(vault.Key(refused), []byte("refused")); err != nil {
			return fmt.Errorf("put refused record: %w", err)
		}

		return nil
	})
	if !errors.Is(err, refusal) {
		t.Fatalf("update error = %v, want the refused shard commit", err)
	}
	commitTx = savedCommit
	assertSameRecords(t, map[string]string{kept: "kept"}, readAllRecords(t, e))
}
