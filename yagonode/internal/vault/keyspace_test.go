package vault

import (
	"context"
	"errors"
	"testing"
)

type failingKeyspaceCodec struct {
	encode error
	decode error
}

func (c failingKeyspaceCodec) Encode(value string) ([]byte, error) {
	return []byte(value), c.encode
}

func (c failingKeyspaceCodec) Decode(raw []byte) (string, error) {
	return string(raw), c.decode
}

func newScriptedKeyspace(
	data *scriptedBucket,
	writable bool,
	codec Codec[string],
) (*Keyspace[string], *Txn) {
	return &Keyspace[string]{name: "data", codec: codec}, &Txn{etx: scriptedTxn{
		writable: writable,
		buckets:  map[Name]*scriptedBucket{"data": data},
	}}
}

func TestRegisterKeyspaceProvisionAndRegistrationErrors(t *testing.T) {
	sentinel := errors.New("provision failed")
	v := &Vault{
		engine:     &scriptedEngine{provisionErr: sentinel},
		registered: map[Name]struct{}{},
	}
	if _, err := RegisterKeyspace(v, "data", internalStringCodec{}); !errors.Is(err, sentinel) {
		t.Fatalf("provision error = %v, want %v", err, sentinel)
	}
	_, err := RegisterKeyspace[string](nil, "data", internalStringCodec{})
	if !errors.Is(err, errVaultClosed) {
		t.Fatalf("closed error = %v, want %v", err, errVaultClosed)
	}
	engine := &scriptedEngine{}
	v, err = New(engine)
	if err != nil {
		t.Fatal(err)
	}
	keyspace, err := RegisterKeyspace(v, "data", internalStringCodec{})
	if err != nil || keyspace.name != "data" {
		t.Fatalf("registered keyspace = %#v, %v", keyspace, err)
	}
	_, err = RegisterKeyspace(v, "data", internalStringCodec{})
	if !errors.Is(err, errDuplicateBucket) {
		t.Fatalf("duplicate error = %v, want %v", err, errDuplicateBucket)
	}
}

func TestKeyspacePutGetContainsAndDelete(t *testing.T) {
	data := &scriptedBucket{values: map[string][]byte{}}
	keyspace, write := newScriptedKeyspace(data, true, internalStringCodec{})
	if err := keyspace.Put(write, Key("key"), "value"); err != nil {
		t.Fatal(err)
	}
	value, found, err := keyspace.Get(write, Key("key"))
	if err != nil || !found || value != "value" || !keyspace.Contains(write, Key("key")) {
		t.Fatalf("get/contains = %q/%t/%v", value, found, err)
	}
	if _, found, err := keyspace.Get(write, Key("missing")); err != nil || found {
		t.Fatalf("missing get = %t/%v", found, err)
	}
	removed, err := keyspace.Delete(write, Key("key"))
	if err != nil || !removed || keyspace.Contains(write, Key("key")) {
		t.Fatalf("delete = %t/%v", removed, err)
	}
	if removed, err := keyspace.Delete(write, Key("missing")); err != nil || removed {
		t.Fatalf("missing delete = %t/%v", removed, err)
	}
	direct := &directPresenceBucket{scriptedBucket: data}
	direct.values["direct"] = []byte("value")
	directTxn := &Txn{etx: presenceTxn{bucket: direct}}
	if !keyspace.Contains(directTxn, Key("direct")) || direct.checks != 1 {
		t.Fatalf("direct presence checks = %d", direct.checks)
	}
}

func TestKeyspaceMutationsLeaveLengthCountersUntouched(t *testing.T) {
	engine := &scriptedEngine{}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	keyspace, err := RegisterKeyspace(v, "data", internalStringCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := putLength(engine.buckets[lengthBucket], Key("data"), 73); err != nil {
		t.Fatal(err)
	}
	assertLength := func() {
		t.Helper()
		length, err := decodeLength(engine.buckets[lengthBucket].Get(Key("data")))
		if err != nil || length != 73 {
			t.Fatalf("length = %d, %v", length, err)
		}
	}
	update := func(operation func(*Txn) error) {
		t.Helper()
		if err := v.Update(context.Background(), operation); err != nil {
			t.Fatal(err)
		}
		assertLength()
	}
	update(func(tx *Txn) error {
		return keyspace.Put(tx, Key("key"), "value")
	})
	update(func(tx *Txn) error {
		return keyspace.Put(tx, Key("key"), "updated")
	})
	update(func(tx *Txn) error {
		_, err := keyspace.Delete(tx, Key("key"))

		return err
	})
	update(func(tx *Txn) error {
		_, err := keyspace.Delete(tx, Key("missing"))

		return err
	})
}

func TestKeyspaceRejectsReadOnlyAndStorageFailures(t *testing.T) {
	data := &scriptedBucket{values: map[string][]byte{"key": []byte("value")}}
	keyspace, read := newScriptedKeyspace(data, false, internalStringCodec{})
	if err := keyspace.Put(read, Key("key"), "new"); !errors.Is(err, errReadOnly) {
		t.Fatalf("read-only put = %v", err)
	}
	if _, err := keyspace.Delete(read, Key("key")); !errors.Is(err, errReadOnly) {
		t.Fatalf("read-only delete = %v", err)
	}
	sentinel := errors.New("codec failed")
	keyspace, write := newScriptedKeyspace(data, true, failingKeyspaceCodec{encode: sentinel})
	if err := keyspace.Put(write, Key("key"), "new"); !errors.Is(err, sentinel) {
		t.Fatalf("encode error = %v, want %v", err, sentinel)
	}
	keyspace.codec = failingKeyspaceCodec{decode: sentinel}
	if _, _, err := keyspace.Get(write, Key("key")); !errors.Is(err, sentinel) {
		t.Fatalf("decode error = %v, want %v", err, sentinel)
	}
	data.putErr = sentinel
	keyspace.codec = internalStringCodec{}
	if err := keyspace.Put(write, Key("key"), "new"); !errors.Is(err, sentinel) {
		t.Fatalf("put error = %v, want %v", err, sentinel)
	}
	data.putErr = nil
	data.deleteErr = sentinel
	if _, err := keyspace.Delete(write, Key("key")); !errors.Is(err, sentinel) {
		t.Fatalf("delete error = %v, want %v", err, sentinel)
	}
}
