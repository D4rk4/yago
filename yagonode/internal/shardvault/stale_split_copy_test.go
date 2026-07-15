package shardvault

import (
	"fmt"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestStaleSplitCopyAppearsOnceInScanAndPages(t *testing.T) {
	shards, vaulted, values := openStaleSplitVault(t)
	want := storeStaleSplitValues(t, vaulted, values, 300)
	if moved, err := shards.SplitStep(t.Context()); err != nil || !moved {
		t.Fatalf("split = %v, %v", moved, err)
	}
	staleKey := selectStaleSplitKey(t, shards, want, 3)
	restoreStaleSplitCopy(t, shards, staleKey, want[staleKey])

	scanKeys := scanStaleSplitKeys(t, vaulted, values)
	assertStaleSplitKeys(t, scanKeys, len(want), staleKey)
	pageKeys := readStaleSplitPageKeys(t, vaulted, 3, len(want))
	assertStaleSplitKeys(t, pageKeys, len(want), staleKey)
}

func openStaleSplitVault(
	t *testing.T,
) (*engine, *vault.Vault, *vault.Collection[string]) {
	t.Helper()
	shards, err := openEngine(filepath.Join(t.TempDir(), "vault"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	vaulted, err := vaultOverEngine(shards)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })
	values, err := vault.Register(vaulted, testBucket, stringCodec{})
	if err != nil {
		t.Fatal(err)
	}

	return shards, vaulted, values
}

func storeStaleSplitValues(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	total int,
) map[string]string {
	t.Helper()
	want := make(map[string]string, total)
	if err := vaulted.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range total {
			key := fmt.Sprintf("key-%05d", index)
			value := fmt.Sprintf("value-%05d", index)
			want[key] = value
			if err := values.Put(tx, vault.Key(key), value); err != nil {
				return fmt.Errorf("put %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return want
}

func selectStaleSplitKey(
	t *testing.T,
	shards *engine,
	want map[string]string,
	pageSize int,
) string {
	t.Helper()
	for index := range len(want) {
		key := fmt.Sprintf("key-%05d", index)
		if index%pageSize != pageSize-1 && shards.route(testBucket, vault.Key(key)) == 8 {
			return key
		}
	}
	t.Fatal("no suitable moved key")

	return ""
}

func restoreStaleSplitCopy(t *testing.T, shards *engine, key, value string) {
	t.Helper()
	if err := shards.shards[0].Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(testBucket))
		if bucket == nil {
			return fmt.Errorf("bucket %s missing", testBucket)
		}

		return bucket.Put([]byte(key), encodeValue([]byte(value)))
	}); err != nil {
		t.Fatal(err)
	}
}

func scanStaleSplitKeys(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
) []string {
	t.Helper()
	var keys []string
	if err := vaulted.View(t.Context(), func(tx *vault.Txn) error {
		return values.Scan(tx, nil, func(key vault.Key, _ string) (bool, error) {
			keys = append(keys, string(key))

			return true, nil
		})
	}); err != nil {
		t.Fatal(err)
	}

	return keys
}

func readStaleSplitPageKeys(
	t *testing.T,
	vaulted *vault.Vault,
	pageSize int,
	total int,
) []string {
	t.Helper()
	var after vault.Key
	var keys []string
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatal("pagination did not finish")
		}
		var page vault.BucketPage
		if err := vaulted.View(t.Context(), func(tx *vault.Txn) error {
			read, err := tx.ReadBucketPage(testBucket, after, pageSize)
			page = read
			if err != nil {
				return fmt.Errorf("read page: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Entries {
			keys = append(keys, string(entry.Key))
		}
		if !page.More {
			return keys
		}
		after = page.Entries[len(page.Entries)-1].Key
	}
}

func assertStaleSplitKeys(t *testing.T, keys []string, total int, staleKey string) {
	t.Helper()
	if len(keys) != total {
		t.Fatalf("keys = %d, want %d", len(keys), total)
	}
	staleOccurrences := 0
	for index, key := range keys {
		if index > 0 && keys[index-1] >= key {
			t.Fatalf("keys did not advance at %q", key)
		}
		if key == staleKey {
			staleOccurrences++
		}
	}
	if staleOccurrences != 1 {
		t.Fatalf("stale key occurrences = %d", staleOccurrences)
	}
}
