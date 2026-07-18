package shardvault

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestConcurrentSameCollectionUpdatesOnDisjointShardsOverlap(t *testing.T) {
	vaulted, shards := openCollectionLengthTestVault(t)
	values, err := vault.Register[string](vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	first, second := disjointCollectionKeys(t, shards, "docs")
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	writers := make(chan error, 2)
	start := func(key vault.Key, mine, other chan struct{}) {
		writers <- vaulted.Update(context.Background(), func(txn *vault.Txn) error {
			if err := values.Put(txn, key, string(key)); err != nil {
				return fmt.Errorf("put concurrent value: %w", err)
			}
			close(mine)
			select {
			case <-other:
				return nil
			case <-time.After(5 * time.Second):
				return errors.New("same-collection writers were serialized")
			}
		})
	}
	go start(first, firstStarted, secondStarted)
	go start(second, secondStarted, firstStarted)
	for range 2 {
		if err := <-writers; err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}
	assertCollectionLength(t, vaulted, values, 2)
}

func TestCollectionLengthCombinesLegacyBaseAndShardChangesAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	shards, err := openEngine(dir, 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	vaulted, err := vaultOverEngine(shards)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	if err := shards.Provision("docs"); err != nil {
		t.Fatalf("provision docs: %v", err)
	}
	var legacy [8]byte
	binary.BigEndian.PutUint64(legacy[:], 2)
	err = shards.Update(context.Background(), func(txn vault.EngineTxn) error {
		if err := txn.Bucket("docs").Put(vault.Key("old-a"), []byte("a")); err != nil {
			return fmt.Errorf("put first legacy value: %w", err)
		}
		if err := txn.Bucket("docs").Put(vault.Key("old-b"), []byte("b")); err != nil {
			return fmt.Errorf("put second legacy value: %w", err)
		}

		if err := txn.Bucket("__lengths__").Put(vault.Key("docs"), legacy[:]); err != nil {
			return fmt.Errorf("put legacy length: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("seed legacy collection: %v", err)
	}
	values, err := vault.Register[string](vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	err = vaulted.Update(context.Background(), func(txn *vault.Txn) error {
		if err := values.Put(txn, vault.Key("new"), "new"); err != nil {
			return fmt.Errorf("put new value: %w", err)
		}
		_, err := values.Delete(txn, vault.Key("old-a"))
		if err != nil {
			return fmt.Errorf("delete legacy value: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("update upgraded collection: %v", err)
	}
	assertCollectionLength(t, vaulted, values, 2)
	if err := vaulted.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := Open(dir, 1<<20)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	values, err = vault.Register[string](reopened, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	assertCollectionLength(t, reopened, values, 2)
}

func TestCollectionLengthSurvivesLinearHashSplit(t *testing.T) {
	vaulted, shards := openCollectionLengthTestVault(t)
	collection, moved, removed := collectionLengthSplitTargets(t, shards)
	values, err := vault.Register[string](vaulted, collection, stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	mutateCollectionKeys(t, vaulted, values, "put", moved, removed)
	mutateCollectionKeys(t, vaulted, values, "delete", removed)
	assertCollectionLength(t, vaulted, values, 1)
	source := shards.split
	target := len(shards.shards)
	level := shards.level
	if !movesTo(
		vault.Name(collectionLengthChangesBucket),
		vault.Key(collection),
		level,
		target,
	) || movesDuringSplit(
		vault.Name(collectionLengthChangesBucket),
		vault.Key(collection),
		level,
		target,
	) {
		t.Fatal("collection length journal was not pinned")
	}
	before := readRawCollectionLengthChanges(t, shards.shards[source], collection)
	if additions, removals, err := decodeCollectionLengthChanges(before); err != nil ||
		additions != 2 || removals != 1 {
		t.Fatalf("changes before split = %d/%d, %v", additions, removals, err)
	}
	grew, err := shards.SplitStep(context.Background())
	if err != nil || !grew {
		t.Fatalf("split = %v, %v", grew, err)
	}
	assertCollectionLength(t, vaulted, values, 1)
	after := readRawCollectionLengthChanges(t, shards.shards[source], collection)
	if string(after) != string(before) {
		t.Fatalf("source changes after split = %x, want %x", after, before)
	}
	copied := readRawCollectionLengthChanges(t, shards.shards[target], collection)
	if copied != nil {
		t.Fatalf("target copied collection length changes: %x", copied)
	}
	if err := vaulted.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := Open(shards.dir, 1<<20)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	values, err = vault.Register[string](reopened, collection, stringCodec{})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	assertCollectionLength(t, reopened, values, 1)
}

func TestCollectionLengthRetryAfterPartialCommitDoesNotDoubleApplyChanges(t *testing.T) {
	t.Run("put", func(t *testing.T) {
		assertCollectionLengthAfterPartialCommitRetry(t, "put", 2, false)
	})
	t.Run("delete", func(t *testing.T) {
		assertCollectionLengthAfterPartialCommitRetry(t, "delete", 0, true)
	})
}

func TestCollectionLengthReportsLegacyAndShardChangeCorruption(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		assertCollectionLengthCorruption(t, func(shards *engine) error {
			return shards.Update(context.Background(), func(txn vault.EngineTxn) error {
				lengths := txn.Bucket("__lengths__")
				if err := lengths.Put(vault.Key("docs"), []byte("bad")); err != nil {
					return fmt.Errorf("put corrupt legacy length: %w", err)
				}

				return nil
			})
		})
	})
	t.Run("shard-change", func(t *testing.T) {
		assertCollectionLengthCorruption(t, func(shards *engine) error {
			return shards.shards[0].Update(func(tx *bolt.Tx) error {
				bucket, err := tx.CreateBucketIfNotExists([]byte(collectionLengthChangesBucket))
				if err != nil {
					return fmt.Errorf("create corrupt change bucket: %w", err)
				}

				if err := bucket.Put([]byte("docs"), []byte("bad")); err != nil {
					return fmt.Errorf("put corrupt shard change: %w", err)
				}

				return nil
			})
		})
	})
}

func TestRecordCollectionLengthChangeSurfacesStorageFailures(t *testing.T) {
	t.Run("contention", func(t *testing.T) {
		assertRecordCollectionLengthChangeFailure(t, func(shards *engine, shard int) {
			shards.shardLocks[shard].Lock()
			t.Cleanup(func() { shards.shardLocks[shard].Unlock() })
		})
	})
	t.Run("create", func(t *testing.T) {
		assertRecordCollectionLengthChangeFailure(t, func(*engine, int) {
			createCollectionLengthChangesBucket = func(*bolt.Tx, []byte) (*bolt.Bucket, error) {
				return nil, errors.New("create failed")
			}
		})
	})
	t.Run("decode", func(t *testing.T) {
		assertRecordCollectionLengthChangeFailure(t, func(shards *engine, shard int) {
			putRawCollectionLengthChanges(t, shards.shards[shard], []byte("bad"))
		})
	})
	t.Run("store", func(t *testing.T) {
		assertRecordCollectionLengthChangeFailure(t, func(*engine, int) {
			storeCollectionLengthChanges = func(*bolt.Bucket, []byte, []byte) error {
				return errors.New("store failed")
			}
		})
	})
}

func TestRecordCollectionLengthChangeRejectsCounterOverflow(t *testing.T) {
	for _, change := range []string{"addition", "removal"} {
		t.Run(change, func(t *testing.T) {
			_, shards := openCollectionLengthTestVault(t)
			key := vault.Key("key")
			shard := shards.route("docs", key)
			additions, removals := uint64(0), uint64(0)
			if change == "addition" {
				additions = ^uint64(0)
			} else {
				removals = ^uint64(0)
			}
			putRawCollectionLengthChanges(t, shards.shards[shard], encodeCollectionLengthChanges(
				additions,
				removals,
			))
			txn := newCollectionLengthShardTxn(t, shards)
			var err error
			if change == "addition" {
				err = txn.RecordCollectionAddition("docs", key)
			} else {
				err = txn.RecordCollectionRemoval("docs", key)
			}
			if err == nil {
				t.Fatalf("%s overflow was accepted", change)
			}
		})
	}
}

func TestCollectionLengthChangesRejectsAggregateOverflow(t *testing.T) {
	for _, overflow := range []string{"aggregate", "platform-int"} {
		t.Run(overflow, func(t *testing.T) {
			_, shards := openCollectionLengthTestVault(t)
			if overflow == "aggregate" {
				putRawCollectionLengthChanges(
					t,
					shards.shards[0],
					encodeCollectionLengthChanges(^uint64(0), 0),
				)
				putRawCollectionLengthChanges(
					t,
					shards.shards[1],
					encodeCollectionLengthChanges(1, 0),
				)
			} else {
				putRawCollectionLengthChanges(
					t,
					shards.shards[0],
					encodeCollectionLengthChanges(uint64(^uint(0)>>1)+1, 0),
				)
			}
			txn := newCollectionLengthShardTxn(t, shards)
			if _, _, err := txn.CollectionLengthChanges("docs"); err == nil {
				t.Fatalf("%s overflow was accepted", overflow)
			}
		})
	}
}

func TestCollectionLengthChangesSurfacesShardContention(t *testing.T) {
	_, shards := openCollectionLengthTestVault(t)
	shards.shardLocks[0].Lock()
	t.Cleanup(func() { shards.shardLocks[0].Unlock() })
	txn := newCollectionLengthShardTxn(t, shards)
	if _, _, err := txn.CollectionLengthChanges("docs"); err == nil {
		t.Fatal("shard contention was accepted")
	}
}

func assertCollectionLengthAfterPartialCommitRetry(
	t *testing.T,
	operation string,
	want int,
	seed bool,
) {
	t.Helper()
	vaulted, shards := openCollectionLengthTestVault(t)
	values, err := vault.Register[string](vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	first, second := disjointCollectionKeys(t, shards, "docs")
	if seed {
		mutateCollectionKeys(t, vaulted, values, "put", first, second)
	}
	originalCommit := commitTx
	t.Cleanup(func() { commitTx = originalCommit })
	commits := 0
	commitTx = func(tx *bolt.Tx) error {
		commits++
		if commits == 2 {
			return errors.New("commit interrupted")
		}

		return originalCommit(tx)
	}
	err = updateCollectionKeys(vaulted, values, operation, first, second)
	if err == nil {
		t.Fatal("partial commit succeeded")
	}
	commitTx = originalCommit
	mutateCollectionKeys(t, vaulted, values, operation, first, second)
	assertCollectionLength(t, vaulted, values, want)
}

func assertCollectionLengthCorruption(
	t *testing.T,
	corrupt func(*engine) error,
) {
	t.Helper()
	vaulted, shards := openCollectionLengthTestVault(t)
	values, err := vault.Register[string](vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := corrupt(shards); err != nil {
		t.Fatalf("corrupt counter: %v", err)
	}
	err = vaulted.View(context.Background(), func(txn *vault.Txn) error {
		if _, err := values.Len(txn); err != nil {
			return fmt.Errorf("read corrupt length: %w", err)
		}

		return nil
	})
	if err == nil {
		t.Fatal("counter corruption was accepted")
	}
}

func assertRecordCollectionLengthChangeFailure(
	t *testing.T,
	prepare func(*engine, int),
) {
	t.Helper()
	_, shards := openCollectionLengthTestVault(t)
	key := vault.Key("key")
	shard := shards.route("docs", key)
	originalCreate := createCollectionLengthChangesBucket
	originalStore := storeCollectionLengthChanges
	t.Cleanup(func() {
		createCollectionLengthChangesBucket = originalCreate
		storeCollectionLengthChanges = originalStore
	})
	prepare(shards, shard)
	txn := newCollectionLengthShardTxn(t, shards)
	if err := txn.RecordCollectionAddition("docs", key); err == nil {
		t.Fatal("storage failure was accepted")
	}
}

func BenchmarkConcurrentSameCollectionWrites(b *testing.B) {
	vaulted, err := Open(filepath.Join(b.TempDir(), "vault"), 1<<30)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { _ = vaulted.Close() })
	vaulted.SetDeferredFsync(true)
	values, err := vault.Register[string](vaulted, "docs", stringCodec{})
	if err != nil {
		b.Fatalf("register: %v", err)
	}
	var sequence atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			key := vault.Key("doc-" + strconv.FormatUint(sequence.Add(1), 10))
			if err := vaulted.Update(context.Background(), func(txn *vault.Txn) error {
				if err := values.Put(txn, key, "value"); err != nil {
					return fmt.Errorf("put benchmark value: %w", err)
				}

				return nil
			}); err != nil {
				b.Errorf("update: %v", err)
				return
			}
		}
	})
}

func BenchmarkCollectionLengthAcrossShards(b *testing.B) {
	vaulted, err := Open(filepath.Join(b.TempDir(), "vault"), 1<<30)
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { _ = vaulted.Close() })
	values, err := vault.Register[string](vaulted, "docs", stringCodec{})
	if err != nil {
		b.Fatalf("register: %v", err)
	}
	mutate := func(txn *vault.Txn) error {
		if err := values.Put(txn, vault.Key("doc"), "value"); err != nil {
			return fmt.Errorf("put benchmark value: %w", err)
		}

		return nil
	}
	if err := vaulted.Update(context.Background(), mutate); err != nil {
		b.Fatalf("seed: %v", err)
	}
	b.ReportMetric(float64(minShards), "shards")
	b.ResetTimer()
	for range b.N {
		err := vaulted.View(context.Background(), func(txn *vault.Txn) error {
			length, err := values.Len(txn)
			if err != nil {
				return fmt.Errorf("read benchmark length: %w", err)
			}
			if length != 1 {
				return fmt.Errorf("length = %d, want 1", length)
			}

			return nil
		})
		if err != nil {
			b.Fatalf("view: %v", err)
		}
	}
}

func openCollectionLengthTestVault(t *testing.T) (*vault.Vault, *engine) {
	t.Helper()
	shards, err := openEngine(filepath.Join(t.TempDir(), "vault"), 1<<20)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	vaulted, err := vaultOverEngine(shards)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })

	return vaulted, shards
}

func disjointCollectionKeys(
	t *testing.T,
	shards *engine,
	collection vault.Name,
) (vault.Key, vault.Key) {
	t.Helper()
	first := vault.Key("key-0")
	firstShard := shards.route(collection, first)
	for index := 1; index < 4096; index++ {
		second := vault.Key(fmt.Sprintf("key-%d", index))
		if shards.route(collection, second) != firstShard {
			return first, second
		}
	}
	t.Fatal("no disjoint collection keys found")

	return nil, nil
}

func collectionLengthSplitTargets(
	t *testing.T,
	shards *engine,
) (vault.Name, vault.Key, vault.Key) {
	t.Helper()
	target := len(shards.shards)
	for candidate := range 4096 {
		collection := vault.Name(fmt.Sprintf("split-docs-%d", candidate))
		if !movesTo(
			vault.Name(collectionLengthChangesBucket),
			vault.Key(collection),
			shards.level,
			target,
		) {
			continue
		}
		var moved vault.Key
		var removed vault.Key
		for index := range 4096 {
			key := vault.Key(fmt.Sprintf("key-%d", index))
			if shards.route(collection, key) != shards.split {
				continue
			}
			if moved == nil && movesTo(collection, key, shards.level, target) {
				moved = key
				continue
			}
			if removed == nil {
				removed = key
			}
			if moved != nil && removed != nil {
				return collection, moved, removed
			}
		}
	}
	t.Fatal("no collection length split targets found")

	return "", nil, nil
}

func mutateCollectionKeys(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	operation string,
	keys ...vault.Key,
) {
	t.Helper()
	if err := updateCollectionKeys(vaulted, values, operation, keys...); err != nil {
		t.Fatalf("%s keys: %v", operation, err)
	}
}

func updateCollectionKeys(
	vaulted *vault.Vault,
	values *vault.Collection[string],
	operation string,
	keys ...vault.Key,
) error {
	err := vaulted.Update(context.Background(), func(txn *vault.Txn) error {
		for _, key := range keys {
			if operation == "put" {
				if err := values.Put(txn, key, string(key)); err != nil {
					return fmt.Errorf("put collection key: %w", err)
				}
				continue
			}
			if _, err := values.Delete(txn, key); err != nil {
				return fmt.Errorf("delete collection key: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update collection keys: %w", err)
	}

	return nil
}

func assertCollectionLength(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	want int,
) {
	t.Helper()
	err := vaulted.View(context.Background(), func(txn *vault.Txn) error {
		got, err := values.Len(txn)
		if err != nil {
			return fmt.Errorf("read collection length: %w", err)
		}
		if got != want {
			return fmt.Errorf("length = %d, want %d", got, want)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read length: %v", err)
	}
}

func newCollectionLengthShardTxn(t *testing.T, shards *engine) *shardTxn {
	t.Helper()
	txn := &shardTxn{
		engine:   shards,
		writable: true,
		tryLocks: true,
		open:     make([]*bolt.Tx, len(shards.shards)),
	}
	t.Cleanup(func() {
		txn.rollback()
		txn.releaseLocks()
	})

	return txn
}

func putRawCollectionLengthChanges(
	t *testing.T,
	database *bolt.DB,
	raw []byte,
) {
	t.Helper()
	err := database.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(collectionLengthChangesBucket))
		if err != nil {
			return fmt.Errorf("create collection length changes: %w", err)
		}

		if err := bucket.Put([]byte("docs"), raw); err != nil {
			return fmt.Errorf("put collection length changes: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("seed collection length changes: %v", err)
	}
}

func readRawCollectionLengthChanges(
	t *testing.T,
	database *bolt.DB,
	collection vault.Name,
) []byte {
	t.Helper()
	var raw []byte
	err := database.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(collectionLengthChangesBucket))
		if bucket != nil {
			raw = append([]byte(nil), bucket.Get([]byte(collection))...)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read collection length changes: %v", err)
	}

	return raw
}
