package shardvault

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestShardReadValueExposesRepairableCorruption(t *testing.T) {
	engine := openTestEngine(t)
	key := vault.Key("corrupt")
	storeCorruptShardValue(t, engine, testBucket, key)
	err := engine.Update(t.Context(), func(tx vault.EngineTxn) error {
		bucket := tx.Bucket(testBucket).(*shardBucket)
		_, found, err := bucket.ReadValue(key)
		if !found || !errors.Is(err, vault.ErrCorruptValue) {
			return fmt.Errorf("read corrupt value: found %t: %w", found, err)
		}
		if err := bucket.Delete(key); err != nil {
			return fmt.Errorf("delete corrupt value: %w", err)
		}

		return bucket.Put(key, []byte("repaired"))
	})
	if err != nil {
		t.Fatal(err)
	}
	err = engine.View(t.Context(), func(tx vault.EngineTxn) error {
		value, found, err := tx.Bucket(testBucket).(*shardBucket).ReadValue(key)
		if err != nil || !found || string(value) != "repaired" {
			return fmt.Errorf("repaired value = %q/%t: %w", value, found, err)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShardDirectGetErrorPropagatesAndRollsBackUpdate(t *testing.T) {
	engine := openTestEngine(t)
	key := vault.Key("corrupt")
	storeCorruptShardValue(t, engine, testBucket, key)
	err := engine.View(t.Context(), func(tx vault.EngineTxn) error {
		tx.Bucket(testBucket).Get(key)

		return nil
	})
	if !errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("view error = %v", err)
	}
	err = engine.Update(t.Context(), func(tx vault.EngineTxn) error {
		bucket := tx.Bucket(testBucket)
		bucket.Get(key)

		return bucket.Put(vault.Key("later"), []byte("must roll back"))
	})
	if !errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("update error = %v", err)
	}
	err = engine.View(t.Context(), func(tx vault.EngineTxn) error {
		if tx.Bucket(testBucket).(*shardBucket).Contains(vault.Key("later")) {
			return errors.New("mutation committed after failed read")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShardIgnoredOperationalReadValueErrorPropagatesAndRollsBack(t *testing.T) {
	engine := openTestEngine(t)
	failedKey, markerKey := keysOnDifferentShards(engine, testBucket)
	failedShard := engine.route(testBucket, failedKey)
	if err := engine.shards[failedShard].Close(); err != nil {
		t.Fatal(err)
	}
	err := engine.View(t.Context(), func(tx vault.EngineTxn) error {
		_, _, _ = tx.Bucket(testBucket).(*shardBucket).ReadValue(failedKey)

		return nil
	})
	assertClosedShardOperationalError(t, err)
	err = engine.Update(t.Context(), func(tx vault.EngineTxn) error {
		_, _, _ = tx.Bucket(testBucket).(*shardBucket).ReadValue(failedKey)

		return tx.Bucket(testBucket).Put(markerKey, []byte("must roll back"))
	})
	assertClosedShardOperationalError(t, err)
	assertShardValueMissing(t, engine, testBucket, markerKey)
}

func TestShardIgnoredCollectionGetErrorPropagatesAndRollsBack(t *testing.T) {
	engine := openTestEngine(t)
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	values, err := vault.Register(storage, "read-access-values", stringCodec{})
	if err != nil {
		t.Fatal(err)
	}
	markers, err := vault.RegisterKeyspace(storage, "read-access-markers", stringCodec{})
	if err != nil {
		t.Fatal(err)
	}
	failedKey, _ := keysOnDifferentShards(engine, vault.Name("read-access-values"))
	failedShard := engine.route(vault.Name("read-access-values"), failedKey)
	markerKey := keyOnAnotherShard(engine, vault.Name("read-access-markers"), failedShard)
	if err := engine.shards[failedShard].Close(); err != nil {
		t.Fatal(err)
	}
	err = storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, _ = values.Get(tx, failedKey)

		return nil
	})
	assertClosedShardOperationalError(t, err)
	err = storage.Update(t.Context(), func(tx *vault.Txn) error {
		_, _, _ = values.Get(tx, failedKey)

		return markers.Put(tx, markerKey, "must roll back")
	})
	assertClosedShardOperationalError(t, err)
	err = storage.View(t.Context(), func(tx *vault.Txn) error {
		_, found, readErr := markers.Get(tx, markerKey)
		if readErr != nil {
			return fmt.Errorf("read rollback marker: %w", readErr)
		}
		if found {
			return errors.New("collection mutation committed after failed read")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShardHandledCollectionCorruptionCanCommitRepair(t *testing.T) {
	engine := openTestEngine(t)
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	values, err := vault.Register(storage, "repairable-values", stringCodec{})
	if err != nil {
		t.Fatal(err)
	}
	key := vault.Key("corrupt")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return values.Put(tx, key, "original")
	}); err != nil {
		t.Fatal(err)
	}
	storeCorruptShardValue(t, engine, "repairable-values", key)
	err = storage.Update(t.Context(), func(tx *vault.Txn) error {
		_, found, readErr := values.Get(tx, key)
		if !found || !errors.Is(readErr, vault.ErrCorruptValue) {
			return fmt.Errorf("read corrupt collection value: found %t: %w", found, readErr)
		}
		deleted, deleteErr := values.Delete(tx, key)
		if deleteErr != nil {
			return fmt.Errorf("delete corrupt collection value: %w", deleteErr)
		}
		if !deleted {
			return errors.New("corrupt collection value disappeared before repair")
		}

		return values.Put(tx, key, "repaired")
	})
	if err != nil {
		t.Fatal(err)
	}
	err = storage.View(t.Context(), func(tx *vault.Txn) error {
		value, found, readErr := values.Get(tx, key)
		if readErr != nil || !found || value != "repaired" {
			return fmt.Errorf("repaired collection value = %q/%t: %w", value, found, readErr)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func keysOnDifferentShards(engine *engine, bucket vault.Name) (vault.Key, vault.Key) {
	first := vault.Key("read-access-0")
	firstShard := engine.route(bucket, first)

	return first, keyOnAnotherShard(engine, bucket, firstShard)
}

func keyOnAnotherShard(engine *engine, bucket vault.Name, excluded int) vault.Key {
	for sequence := 1; ; sequence++ {
		key := vault.Key(fmt.Sprintf("read-access-%d", sequence))
		if engine.route(bucket, key) != excluded {
			return key
		}
	}
}

func assertShardValueMissing(t *testing.T, engine *engine, bucket vault.Name, key vault.Key) {
	t.Helper()
	err := engine.View(t.Context(), func(tx vault.EngineTxn) error {
		if tx.Bucket(bucket).(*shardBucket).Contains(key) {
			return errors.New("mutation committed after failed read")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertClosedShardOperationalError(t *testing.T, err error) {
	t.Helper()
	if err == nil || errors.Is(err, vault.ErrCorruptValue) ||
		!strings.Contains(err.Error(), "database not open") {
		t.Fatalf("closed shard operational error = %v", err)
	}
}

func storeCorruptShardValue(t *testing.T, engine *engine, bucket vault.Name, key vault.Key) {
	t.Helper()
	shard := engine.route(bucket, key)
	err := engine.shards[shard].Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucket)).Put(key, []byte{0xff})
	})
	if err != nil {
		t.Fatal(err)
	}
}
