package boltvault

import (
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestBoltBucketReadsLastKey(t *testing.T) {
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
		key, err := (boltBucket{bucket: tx.Bucket([]byte("bucket"))}).LastKey()
		if err != nil || string(key) != "e" {
			t.Fatalf("last key = %q, %v", key, err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBoltBucketLastKeyHandlesMissingBucket(t *testing.T) {
	key, err := (boltBucket{}).LastKey()
	if err != nil || key != nil {
		t.Fatalf("missing bucket last key = %q, %v", key, err)
	}
}
