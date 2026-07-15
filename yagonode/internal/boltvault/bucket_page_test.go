package boltvault

import (
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestBoltBucketReadsExclusiveOrderedPages(t *testing.T) {
	db := openTestBolt(t)
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("bucket"))
		for _, key := range []string{"e", "a", "c"} {
			if err := bucket.Put([]byte(key), []byte("value-"+key)); err != nil {
				return fmt.Errorf("put %s: %w", key, err)
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *bolt.Tx) error {
		bucket := boltBucket{bucket: tx.Bucket([]byte("bucket"))}
		assertBoltPage(t, bucket, nil, "a,c", true)
		assertBoltPage(t, bucket, vault.Key("c"), "e", false)
		assertBoltPage(t, bucket, vault.Key("b"), "c,e", false)
		assertBoltPage(t, bucket, vault.Key("z"), "", false)

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertBoltPage(
	t *testing.T,
	bucket boltBucket,
	after vault.Key,
	want string,
	more bool,
) {
	t.Helper()
	page, err := bucket.ReadPageAfter(after, 2)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]byte, 0, len(page.Entries)*2)
	for _, entry := range page.Entries {
		if len(keys) > 0 {
			keys = append(keys, ',')
		}
		keys = append(keys, entry.Key...)
	}
	if string(keys) != want || page.More != more {
		t.Fatalf("page after %q = %q/%t, want %q/%t", after, keys, page.More, want, more)
	}
}
