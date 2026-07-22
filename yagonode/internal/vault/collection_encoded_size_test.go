package vault

import (
	"errors"
	"testing"
)

type sizedBucket struct {
	*scriptedBucket
	size  int
	found bool
	err   error
	calls int
}

func (b *sizedBucket) ValueSize(Key) (int, bool, error) {
	b.calls++

	return b.size, b.found, b.err
}

func TestCollectionEncodedSizeUsesNativeSizer(t *testing.T) {
	sentinel := errors.New("size failed")
	bucket := &sizedBucket{
		scriptedBucket: &scriptedBucket{values: map[string][]byte{"key": []byte("value")}},
		size:           123,
		found:          true,
	}
	collection := &Collection[string]{name: Name("data"), codec: internalStringCodec{}}
	keyspace := &Keyspace[string]{name: Name("data"), codec: internalStringCodec{}}
	size, found, err := collection.EncodedSize(
		&Txn{etx: presenceTxn{bucket: bucket}}, Key("key"),
	)
	if err != nil || !found || size != 123 || bucket.calls != 1 {
		t.Fatalf("native size = %d/%t/%v, calls %d", size, found, err, bucket.calls)
	}
	size, found, err = keyspace.EncodedSize(
		&Txn{etx: presenceTxn{bucket: bucket}}, Key("key"),
	)
	if err != nil || !found || size != 123 || bucket.calls != 2 {
		t.Fatalf("keyspace native size = %d/%t/%v, calls %d", size, found, err, bucket.calls)
	}
	bucket.err = sentinel
	if _, _, err := collection.EncodedSize(
		&Txn{etx: presenceTxn{bucket: bucket}}, Key("key"),
	); !errors.Is(err, sentinel) {
		t.Fatalf("native size error = %v, want %v", err, sentinel)
	}
}

func TestCollectionEncodedSizeFallsBackToRawValue(t *testing.T) {
	bucket := &scriptedBucket{values: map[string][]byte{"key": []byte("value")}}
	collection := &Collection[string]{name: Name("data"), codec: internalStringCodec{}}
	keyspace := &Keyspace[string]{name: Name("data"), codec: internalStringCodec{}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	size, found, err := collection.EncodedSize(tx, Key("key"))
	if err != nil || !found || size != len("value") {
		t.Fatalf("fallback size = %d/%t/%v", size, found, err)
	}
	if size, found, err := collection.EncodedSize(
		tx,
		Key("missing"),
	); err != nil || found ||
		size != 0 {
		t.Fatalf("missing size = %d/%t/%v", size, found, err)
	}
	if size, found, err := keyspace.EncodedSize(
		tx,
		Key("key"),
	); err != nil || !found || size != len("value") {
		t.Fatalf("keyspace fallback size = %d/%t/%v", size, found, err)
	}
}
