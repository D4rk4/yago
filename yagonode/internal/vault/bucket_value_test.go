package vault

import "testing"

func TestReadBucketValueCopiesStoredBytes(t *testing.T) {
	raw := []byte("value")
	bucket := &scriptedBucket{values: map[string][]byte{"key": raw}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	read, found := tx.ReadBucketValue("data", Key("key"))
	if !found || string(read) != "value" {
		t.Fatalf("read = %q/%t", read, found)
	}
	raw[0] = 'x'
	if string(read) != "value" {
		t.Fatalf("copied read = %q", read)
	}
	if read, found := tx.ReadBucketValue("data", Key("missing")); found || read != nil {
		t.Fatalf("missing read = %q/%t", read, found)
	}
}
