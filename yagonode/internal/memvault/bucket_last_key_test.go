package memvault

import "testing"

func TestMemBucketReadsLastKey(t *testing.T) {
	bucket := memBucket{entries: map[string][]byte{
		"e": []byte("value-e"),
		"a": []byte("value-a"),
		"c": []byte("value-c"),
	}}
	key, err := bucket.LastKey()
	if err != nil || string(key) != "e" {
		t.Fatalf("last key = %q, %v", key, err)
	}
}

func TestMemBucketLastKeyHandlesEmptyBucket(t *testing.T) {
	key, err := (memBucket{}).LastKey()
	if err != nil || key != nil {
		t.Fatalf("empty bucket last key = %q, %v", key, err)
	}
}
