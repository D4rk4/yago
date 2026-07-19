package shardvault

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestCleanShutdownPersistsFreelistWithoutRecoveryCommitOnReopen(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vault")
	engine, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if err := engine.Provision("docs"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := engine.Update(t.Context(), func(transaction vault.EngineTxn) error {
		bucket := transaction.Bucket("docs")
		for index := range 128 {
			key := vault.Key(fmt.Sprintf("document-%03d", index))
			if err := bucket.Put(key, []byte("value")); err != nil {
				return fmt.Errorf("store document: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("populate: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	assertFreelistReopensWithoutRecoveryWrite(t, directory)
}

func TestDeferredFsyncCleanShutdownPersistsAcceptedDataAndFreelist(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vault")
	engine, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if err := engine.Provision(testBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}
	engine.SetDeferredFsync(true)
	key := vault.Key("deferred-document")
	want := []byte("accepted before clean shutdown")
	if err := engine.Update(t.Context(), func(transaction vault.EngineTxn) error {
		return transaction.Bucket(testBucket).Put(key, want)
	}); err != nil {
		t.Fatalf("store deferred document: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close deferred engine: %v", err)
	}
	assertFreelistReopensWithoutRecoveryWrite(t, directory)

	reopened, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if err := reopened.View(t.Context(), func(transaction vault.EngineTxn) error {
		if got := transaction.Bucket(testBucket).Get(key); !bytes.Equal(got, want) {
			t.Fatalf("reopened value = %q, want %q", got, want)
		}

		return nil
	}); err != nil {
		t.Fatalf("read reopened engine: %v", err)
	}
}

func assertFreelistReopensWithoutRecoveryWrite(t *testing.T, directory string) {
	t.Helper()
	for shard := range minShards {
		database, err := bolt.Open(
			shardPath(directory, shard),
			0o600,
			freelistObservationOptions(),
		)
		if err != nil {
			t.Fatalf("reopen shard %d: %v", shard, err)
		}
		statistics := database.Stats()
		writes := statistics.TxStats.GetWrite()
		if err := database.Close(); err != nil {
			t.Fatalf("close observed shard %d: %v", shard, err)
		}
		if writes != 0 {
			t.Fatalf("shard %d recovery writes = %d, want 0", shard, writes)
		}
	}
}

func TestUncheckpointedShardReopenRetainsFreelistRecoveryCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shard.vlt")
	database, err := bolt.Open(path, 0o600, openTimeoutOptions())
	if err != nil {
		t.Fatalf("open shard: %v", err)
	}
	if err := database.Update(func(transaction *bolt.Tx) error {
		bucket, err := transaction.CreateBucketIfNotExists([]byte("docs"))
		if err != nil {
			return fmt.Errorf("create documents bucket: %w", err)
		}

		if err := bucket.Put([]byte("document"), []byte("value")); err != nil {
			return fmt.Errorf("store document: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("write shard: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close uncheckpointed shard: %v", err)
	}

	recovered, err := bolt.Open(path, 0o600, freelistObservationOptions())
	if err != nil {
		t.Fatalf("recover shard: %v", err)
	}
	statistics := recovered.Stats()
	writes := statistics.TxStats.GetWrite()
	if err := recovered.Close(); err != nil {
		t.Fatalf("close recovered shard: %v", err)
	}
	if writes == 0 {
		t.Fatal("uncheckpointed shard reopened without a freelist recovery commit")
	}
}

func TestCheckpointShardFreelistCommitsExactlyOneEmptyTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shard.vlt")
	database, err := bolt.Open(path, 0o600, openTimeoutOptions())
	if err != nil {
		t.Fatalf("open shard: %v", err)
	}
	database.NoSync = false
	database.NoFreelistSync = false
	before := shardTransactionIdentity(t, database)
	if err := checkpointShardFreelist(database); err != nil {
		t.Fatalf("checkpoint shard: %v", err)
	}
	after := shardTransactionIdentity(t, database)
	if err := database.Close(); err != nil {
		t.Fatalf("close shard: %v", err)
	}
	if after != before+1 {
		t.Fatalf("transaction identity = %d after %d, want one checkpoint", after, before)
	}
}

func TestCheckpointShardFreelistWrapsClosedDatabaseFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shard.vlt")
	database, err := bolt.Open(path, 0o600, openTimeoutOptions())
	if err != nil {
		t.Fatalf("open shard: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close shard: %v", err)
	}
	if err := checkpointShardFreelist(database); err == nil ||
		!strings.Contains(err.Error(), "persist freelist") {
		t.Fatalf("checkpoint error = %v, want wrapped persistence failure", err)
	}
}

func TestDeferredCheckpointFailureFallsBackToSyncAndClosesEveryShard(t *testing.T) {
	databases := []*bolt.DB{
		{NoSync: true, NoFreelistSync: true},
		{NoSync: true, NoFreelistSync: true},
		{NoSync: true, NoFreelistSync: true},
	}
	shards := map[*bolt.DB]int{
		databases[0]: 0,
		databases[1]: 1,
		databases[2]: 2,
	}
	checkpointZero := errors.New("checkpoint zero")
	checkpointOne := errors.New("checkpoint one")
	syncZero := errors.New("sync zero")
	closeZero := errors.New("close zero")
	events := make([]string, 0)
	operations := shardShutdownOperations{
		checkpoint: func(database *bolt.DB) error {
			shard := shards[database]
			events = append(events, fmt.Sprintf("checkpoint-%d", shard))
			if database.NoSync || database.NoFreelistSync {
				t.Fatalf("shard %d checkpoint retained hot-path flags", shard)
			}
			switch shard {
			case 0:
				return checkpointZero
			case 1:
				return checkpointOne
			default:
				return nil
			}
		},
		sync: func(database *bolt.DB) error {
			shard := shards[database]
			events = append(events, fmt.Sprintf("sync-%d", shard))
			if shard == 0 {
				return syncZero
			}

			return nil
		},
		close: func(database *bolt.DB) error {
			shard := shards[database]
			events = append(events, fmt.Sprintf("close-%d", shard))
			if shard == 0 {
				return closeZero
			}

			return nil
		},
	}

	err := closeShardDatabases(databases, true, operations)
	for _, expected := range []error{checkpointZero, checkpointOne, syncZero, closeZero} {
		if !errors.Is(err, expected) {
			t.Fatalf("close error %v does not include %v", err, expected)
		}
	}
	want := []string{
		"checkpoint-0", "sync-0", "close-0",
		"checkpoint-1", "sync-1", "close-1",
		"checkpoint-2", "close-2",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("shutdown events = %v, want %v", events, want)
	}
}

func TestDurableCheckpointFailureDoesNotRequestFallbackSync(t *testing.T) {
	database := &bolt.DB{NoSync: true, NoFreelistSync: true}
	checkpointFailure := errors.New("checkpoint failed")
	synced := false
	closed := false
	err := closeShardDatabases(
		[]*bolt.DB{database},
		false,
		shardShutdownOperations{
			checkpoint: func(*bolt.DB) error { return checkpointFailure },
			sync: func(*bolt.DB) error {
				synced = true

				return nil
			},
			close: func(*bolt.DB) error {
				closed = true

				return nil
			},
		},
	)
	if !errors.Is(err, checkpointFailure) || synced || !closed {
		t.Fatalf("close outcome = err:%v synced:%t closed:%t", err, synced, closed)
	}
}

func freelistObservationOptions() *bolt.Options {
	return &bolt.Options{
		Timeout:        5 * time.Second,
		NoFreelistSync: false,
		FreelistType:   bolt.FreelistMapType,
	}
}

func shardTransactionIdentity(t *testing.T, database *bolt.DB) int {
	t.Helper()
	identity := 0
	if err := database.View(func(transaction *bolt.Tx) error {
		identity = transaction.ID()

		return nil
	}); err != nil {
		t.Fatalf("read shard transaction identity: %v", err)
	}

	return identity
}
