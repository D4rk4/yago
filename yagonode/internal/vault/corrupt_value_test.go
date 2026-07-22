package vault

import (
	"errors"
	"testing"
)

type corruptValueCodec struct {
	failure error
}

func (c corruptValueCodec) Encode(value string) ([]byte, error) {
	return []byte(value), nil
}

func (c corruptValueCodec) Decode([]byte) (string, error) {
	return "", c.failure
}

type failingValueReader struct {
	*scriptedBucket
	failure error
	found   bool
}

func (b *failingValueReader) ReadValue(Key) ([]byte, bool, error) {
	return nil, b.found, b.failure
}

func TestCollectionDecodeFailureIsStoredValueCorruption(t *testing.T) {
	failure := errors.New("decode failed")
	collection := &Collection[string]{name: "data", codec: corruptValueCodec{failure: failure}}
	bucket := &scriptedBucket{values: map[string][]byte{"key": []byte("invalid")}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	_, found, err := collection.Get(tx, Key("key"))
	if !found || !errors.Is(err, ErrCorruptValue) || !errors.Is(err, failure) {
		t.Fatalf("collection get = found %t, error %v", found, err)
	}
	err = collection.Scan(tx, nil, func(Key, string) (bool, error) {
		return true, nil
	})
	if !errors.Is(err, ErrCorruptValue) || !errors.Is(err, failure) {
		t.Fatalf("collection scan error = %v", err)
	}
}

func TestKeyspaceDecodeFailureIsStoredValueCorruption(t *testing.T) {
	failure := errors.New("decode failed")
	keyspace := &Keyspace[string]{name: "data", codec: corruptValueCodec{failure: failure}}
	bucket := &scriptedBucket{values: map[string][]byte{"key": []byte("invalid")}}
	_, found, err := keyspace.Get(
		&Txn{etx: presenceTxn{bucket: bucket}},
		Key("key"),
	)
	if !found || !errors.Is(err, ErrCorruptValue) || !errors.Is(err, failure) {
		t.Fatalf("keyspace get = found %t, error %v", found, err)
	}
}

func TestEncodedValueReadFailureRemainsOperational(t *testing.T) {
	failure := errors.New("read failed")
	bucket := &failingValueReader{
		scriptedBucket: &scriptedBucket{values: map[string][]byte{"key": []byte("value")}},
		failure:        failure,
		found:          true,
	}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	collection := &Collection[string]{name: "data", codec: internalStringCodec{}}
	_, found, err := collection.Get(tx, Key("key"))
	if !found || !errors.Is(err, failure) || errors.Is(err, ErrCorruptValue) {
		t.Fatalf("collection get = found %t, error %v", found, err)
	}
	keyspace := &Keyspace[string]{name: "data", codec: internalStringCodec{}}
	_, found, err = keyspace.Get(tx, Key("key"))
	if !found || !errors.Is(err, failure) || errors.Is(err, ErrCorruptValue) {
		t.Fatalf("keyspace get = found %t, error %v", found, err)
	}
}
