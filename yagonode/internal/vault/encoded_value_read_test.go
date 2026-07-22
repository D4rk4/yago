package vault

import "testing"

type successfulValueReader struct {
	*scriptedBucket
	raw   []byte
	found bool
}

func (b *successfulValueReader) ReadValue(Key) ([]byte, bool, error) {
	return append([]byte(nil), b.raw...), b.found, nil
}

func TestEncodedValueReaderPreservesSuccessfulPresence(t *testing.T) {
	bucket := &successfulValueReader{
		scriptedBucket: &scriptedBucket{},
		raw:            []byte("value"),
		found:          true,
	}
	collection := &Collection[string]{name: "data", codec: internalStringCodec{}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	value, found, err := collection.Get(tx, Key("key"))
	if err != nil || !found || value != "value" {
		t.Fatalf("present value = %q/%t/%v", value, found, err)
	}
	bucket.raw = nil
	bucket.found = false
	value, found, err = collection.Get(tx, Key("missing"))
	if err != nil || found || value != "" {
		t.Fatalf("missing value = %q/%t/%v", value, found, err)
	}
}
