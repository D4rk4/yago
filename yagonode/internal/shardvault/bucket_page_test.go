package shardvault

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestShardBucketReadsExclusiveOrderedPages(t *testing.T) {
	vaulted, _ := openTestVault(t)
	values, err := vault.Register(vaulted, "paged", stringCodec{})
	if err != nil {
		t.Fatal(err)
	}
	storeShardPageValues(t, vaulted, values)
	keys, views := readAllShardPageKeys(t, vaulted)
	if views != 6 || len(keys) != 16 {
		t.Fatalf("views/keys = %d/%d", views, len(keys))
	}
	if !sortStringsOrdered(keys) || keys[0] != "key-00" || keys[15] != "key-15" {
		t.Fatalf("keys = %s", strings.Join(keys, ","))
	}
}

func storeShardPageValues(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
) {
	t.Helper()
	if err := vaulted.Update(t.Context(), func(tx *vault.Txn) error {
		for index := 15; index >= 0; index-- {
			key := fmt.Sprintf("key-%02d", index)
			if err := values.Put(tx, vault.Key(key), "value-"+key); err != nil {
				return fmt.Errorf("put %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func readAllShardPageKeys(t *testing.T, vaulted *vault.Vault) ([]string, int) {
	t.Helper()
	var after vault.Key
	var keys []string
	views := 0
	for {
		var page vault.BucketPage
		if err := vaulted.View(t.Context(), func(tx *vault.Txn) error {
			read, err := tx.ReadBucketPage("paged", after, 3)
			page = read
			if err != nil {
				return fmt.Errorf("read page: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
		views++
		for _, entry := range page.Entries {
			keys = append(keys, string(entry.Key))
		}
		if !page.More {
			break
		}
		after = page.Entries[len(page.Entries)-1].Key
	}

	return keys, views
}

func sortStringsOrdered(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] >= values[index] {
			return false
		}
	}

	return true
}

func TestShardBucketPageHandlesMissingBucketAndShardError(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "empty.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := &engine{shards: []*bolt.DB{db}}
	txn := &shardTxn{engine: engine, open: make([]*bolt.Tx, 1)}
	page, err := (&shardBucket{txn: txn, name: "missing"}).ReadPageAfter(nil, 1)
	txn.rollback()
	if err != nil || len(page.Entries) != 0 || page.More {
		t.Fatalf("missing bucket page = %#v, %v", page, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	txn = &shardTxn{engine: engine, open: make([]*bolt.Tx, 1)}
	if _, err := (&shardBucket{txn: txn, name: "missing"}).ReadPageAfter(nil, 1); err == nil {
		t.Fatal("closed shard page succeeded")
	}
}

func TestShardBucketPageReportsDecodeError(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "invalid.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("documents"))
		if err != nil {
			return fmt.Errorf("create documents bucket: %w", err)
		}

		return bucket.Put([]byte("a"), []byte{0xff})
	}); err != nil {
		t.Fatal(err)
	}
	engine := &engine{shards: []*bolt.DB{db}}
	txn := &shardTxn{engine: engine, open: make([]*bolt.Tx, 1)}
	_, err = (&shardBucket{txn: txn, name: "documents"}).ReadPageAfter(nil, 1)
	txn.rollback()
	if err == nil {
		t.Fatal("invalid encoded value accepted")
	}
}

func TestFirstKeyAfterPositionsExclusiveCursor(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "cursor.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("documents"))
		if err != nil {
			return fmt.Errorf("create documents bucket: %w", err)
		}
		if err := bucket.Put([]byte("b"), encodeValue([]byte("b"))); err != nil {
			return fmt.Errorf("put b: %w", err)
		}

		if err := bucket.Put([]byte("d"), encodeValue([]byte("d"))); err != nil {
			return fmt.Errorf("put d: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket([]byte("documents")).Cursor()
		key, _ := firstKeyAfter(cursor, vault.Key("b"))
		if string(key) != "d" {
			return errors.New("exact cursor was not exclusive")
		}
		key, _ = firstKeyAfter(cursor, vault.Key("c"))
		if string(key) != "d" {
			return errors.New("missing cursor did not seek forward")
		}
		key, _ = firstKeyAfter(cursor, vault.Key("z"))
		if key != nil {
			return errors.New("terminal cursor returned a key")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
