package vault

import (
	"errors"
	"testing"
)

type lastKeyReaderBucket struct {
	*scriptedBucket
	key Key
	err error
}

func (b *lastKeyReaderBucket) LastKey() (Key, error) {
	return b.key, b.err
}

func TestReadBucketLastKeyCopiesReaderKey(t *testing.T) {
	key := Key("last")
	bucket := &lastKeyReaderBucket{scriptedBucket: &scriptedBucket{}, key: key}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	read, err := tx.ReadBucketLastKey("documents")
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'x'
	if string(read) != "last" {
		t.Fatalf("last key = %q", read)
	}
}

func TestReadBucketLastKeyReturnsEmptyBucket(t *testing.T) {
	bucket := &lastKeyReaderBucket{scriptedBucket: &scriptedBucket{}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	key, err := tx.ReadBucketLastKey("documents")
	if err != nil || key != nil {
		t.Fatalf("empty last key = %q, %v", key, err)
	}
}

func TestReadBucketLastKeyRejectsUnavailableReader(t *testing.T) {
	tx := &Txn{etx: presenceTxn{bucket: &scriptedBucket{}}}
	_, err := tx.ReadBucketLastKey("documents")
	if !errors.Is(err, errBucketLastKeyUnavailable) {
		t.Fatalf("unavailable reader error = %v", err)
	}
}

func TestReadBucketLastKeyWrapsReaderError(t *testing.T) {
	sentinel := errors.New("last key failed")
	bucket := &lastKeyReaderBucket{scriptedBucket: &scriptedBucket{}, err: sentinel}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	if _, err := tx.ReadBucketLastKey("documents"); !errors.Is(err, sentinel) {
		t.Fatalf("reader error = %v, want %v", err, sentinel)
	}
}
