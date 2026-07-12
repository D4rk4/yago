package vault

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

type internalStringCodec struct{}

func (internalStringCodec) Encode(value string) ([]byte, error) {
	return []byte(value), nil
}

func (internalStringCodec) Decode(raw []byte) (string, error) {
	return string(raw), nil
}

type scriptedEngine struct {
	buckets      map[Name]*scriptedBucket
	provisionErr error
	closeErr     error
	usedErr      error
	quota        int64
	used         int64
}

func (e *scriptedEngine) Provision(name Name) error {
	if e.provisionErr != nil {
		return e.provisionErr
	}
	if e.buckets == nil {
		e.buckets = map[Name]*scriptedBucket{}
	}
	if e.buckets[name] == nil {
		e.buckets[name] = &scriptedBucket{values: map[string][]byte{}}
	}
	return nil
}

func (e *scriptedEngine) Update(_ context.Context, fn func(EngineTxn) error) error {
	return fn(scriptedTxn{buckets: e.buckets, writable: true})
}

func (e *scriptedEngine) View(_ context.Context, fn func(EngineTxn) error) error {
	return fn(scriptedTxn{buckets: e.buckets})
}

func (e *scriptedEngine) Close() error {
	return e.closeErr
}

func (e *scriptedEngine) QuotaBytes() int64 {
	return e.quota
}

func (e *scriptedEngine) UsedBytes(context.Context) (int64, error) {
	return e.used, e.usedErr
}

type scriptedTxn struct {
	buckets  map[Name]*scriptedBucket
	writable bool
}

func (t scriptedTxn) Writable() bool {
	return t.writable
}

func (t scriptedTxn) Bucket(name Name) EngineBucket {
	return t.buckets[name]
}

type scriptedBucket struct {
	values    map[string][]byte
	putErr    error
	deleteErr error
	scanErr   error
}

type presenceTxn struct {
	bucket EngineBucket
}

func (t presenceTxn) Bucket(Name) EngineBucket {
	return t.bucket
}

func (presenceTxn) Writable() bool {
	return false
}

type directPresenceBucket struct {
	*scriptedBucket
	checks int
}

func (b *directPresenceBucket) Contains(key Key) bool {
	b.checks++
	_, found := b.values[string(key)]

	return found
}

func (b *scriptedBucket) Get(key Key) []byte {
	return b.values[string(key)]
}

func (b *scriptedBucket) Put(key Key, value []byte) error {
	if b.putErr != nil {
		return b.putErr
	}
	b.values[string(key)] = append([]byte(nil), value...)
	return nil
}

func (b *scriptedBucket) Delete(key Key) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	delete(b.values, string(key))
	return nil
}

func (b *scriptedBucket) Scan(prefix Key, fn func(Key, []byte) (bool, error)) error {
	if b.scanErr != nil {
		return b.scanErr
	}
	for key, value := range b.values {
		if len(prefix) > 0 && key != string(prefix) {
			continue
		}
		keep, err := fn(Key(key), value)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}
	return nil
}

func newScriptedCollection(
	data, lengths *scriptedBucket,
	writable bool,
) (*Collection[string], *Txn) {
	return &Collection[string]{
			name:  Name("data"),
			codec: internalStringCodec{},
		}, &Txn{etx: scriptedTxn{
			writable: writable,
			buckets: map[Name]*scriptedBucket{
				Name("data"): data,
				lengthBucket: lengths,
			},
		}}
}

func TestCollectionContainsUsesPresenceCapabilityAndGetFallback(t *testing.T) {
	collection := &Collection[string]{name: Name("data"), codec: internalStringCodec{}}
	stored := &scriptedBucket{values: map[string][]byte{"key": []byte("value")}}
	direct := &directPresenceBucket{scriptedBucket: stored}
	if !collection.Contains(&Txn{etx: presenceTxn{bucket: direct}}, Key("key")) ||
		direct.checks != 1 {
		t.Fatalf("direct presence checks = %d", direct.checks)
	}
	if !collection.Contains(&Txn{etx: presenceTxn{bucket: stored}}, Key("key")) ||
		collection.Contains(&Txn{etx: presenceTxn{bucket: stored}}, Key("missing")) {
		t.Fatal("fallback presence mismatch")
	}
}

func TestNewReturnsProvisionError(t *testing.T) {
	sentinel := errors.New("provision failed")

	if _, err := New(&scriptedEngine{provisionErr: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("New error = %v, want %v", err, sentinel)
	}
}

func TestRegisterReturnsProvisionError(t *testing.T) {
	sentinel := errors.New("provision failed")
	v := &Vault{
		engine:     &scriptedEngine{provisionErr: sentinel},
		registered: map[Name]struct{}{},
	}

	if _, err := Register(v, Name("data"), internalStringCodec{}); !errors.Is(err, sentinel) {
		t.Fatalf("Register error = %v, want %v", err, sentinel)
	}
}

func TestVaultCloseReturnsEngineError(t *testing.T) {
	sentinel := errors.New("close failed")
	v := &Vault{engine: &scriptedEngine{closeErr: sentinel}}

	if err := v.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("Close error = %v, want %v", err, sentinel)
	}
}

func TestVaultQuotaAndUsage(t *testing.T) {
	v := &Vault{engine: &scriptedEngine{quota: 10, used: 11}}

	if got := v.QuotaBytes(); got != 10 {
		t.Fatalf("QuotaBytes = %d, want 10", got)
	}
	used, err := v.UsedBytes(context.Background())
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}
	if used != 11 {
		t.Fatalf("UsedBytes = %d, want 11", used)
	}
	atCapacity, err := v.AtCapacity(context.Background())
	if err != nil {
		t.Fatalf("AtCapacity: %v", err)
	}
	if !atCapacity {
		t.Fatal("AtCapacity = false, want true")
	}
}

func TestVaultUsedBytesReturnsEngineError(t *testing.T) {
	sentinel := errors.New("measure failed")
	v := &Vault{engine: &scriptedEngine{quota: 10, usedErr: sentinel}}

	if _, err := v.UsedBytes(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("UsedBytes error = %v, want %v", err, sentinel)
	}
	if _, err := v.AtCapacity(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("AtCapacity error = %v, want %v", err, sentinel)
	}
}

func TestNilVaultQuotaBytes(t *testing.T) {
	var v *Vault

	if got := v.QuotaBytes(); got != 0 {
		t.Fatalf("QuotaBytes = %d, want 0", got)
	}
}

func TestCollectionGetReportsMissingKey(t *testing.T) {
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{}},
		&scriptedBucket{values: map[string][]byte{}},
		false,
	)

	_, ok, err := collection.Get(tx, Key("missing"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("Get found missing key")
	}
}

func TestCollectionPutHandlesExistingKey(t *testing.T) {
	lengths := &scriptedBucket{values: map[string][]byte{}}
	if err := putLength(lengths, Key("data"), 1); err != nil {
		t.Fatalf("put length: %v", err)
	}
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{"key": []byte("old")}},
		lengths,
		true,
	)

	if err := collection.Put(tx, Key("key"), "new"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	length, err := collection.Len(tx)
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if length != 1 {
		t.Fatalf("Len = %d, want 1", length)
	}
}

func TestCollectionPutReturnsStoreError(t *testing.T) {
	sentinel := errors.New("put failed")
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{}, putErr: sentinel},
		&scriptedBucket{values: map[string][]byte{}},
		true,
	)

	if err := collection.Put(tx, Key("key"), "value"); !errors.Is(err, sentinel) {
		t.Fatalf("Put error = %v, want %v", err, sentinel)
	}
}

func TestCollectionPutReturnsLengthDecodeError(t *testing.T) {
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{}},
		&scriptedBucket{values: map[string][]byte{"data": []byte("bad")}},
		true,
	)

	if err := collection.Put(tx, Key("key"), "value"); err == nil {
		t.Fatal("expected length decode error")
	}
}

func TestCollectionDeletePaths(t *testing.T) {
	data := &scriptedBucket{values: map[string][]byte{"key": []byte("value")}}
	lengths := &scriptedBucket{values: map[string][]byte{}}
	if err := putLength(lengths, Key("data"), 1); err != nil {
		t.Fatalf("put length: %v", err)
	}
	collection, tx := newScriptedCollection(data, lengths, true)

	missing, err := collection.Delete(tx, Key("missing"))
	if err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
	if missing {
		t.Fatal("Delete reported missing key as deleted")
	}
	deleted, err := collection.Delete(tx, Key("key"))
	if err != nil {
		t.Fatalf("Delete existing: %v", err)
	}
	if !deleted {
		t.Fatal("Delete did not report existing key as deleted")
	}
	length, err := collection.Len(tx)
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if length != 0 {
		t.Fatalf("Len = %d, want 0", length)
	}
}

func TestCollectionDeleteReturnsDeleteError(t *testing.T) {
	sentinel := errors.New("delete failed")
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{"key": []byte("value")}, deleteErr: sentinel},
		&scriptedBucket{values: map[string][]byte{}},
		true,
	)

	if _, err := collection.Delete(tx, Key("key")); !errors.Is(err, sentinel) {
		t.Fatalf("Delete error = %v, want %v", err, sentinel)
	}
}

func TestCollectionDeleteReturnsLengthError(t *testing.T) {
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{"key": []byte("value")}},
		&scriptedBucket{values: map[string][]byte{"data": []byte("bad")}},
		true,
	)

	if _, err := collection.Delete(tx, Key("key")); err == nil {
		t.Fatal("expected length error")
	}
}

func TestCollectionScanReturnsEngineError(t *testing.T) {
	sentinel := errors.New("scan failed")
	collection, tx := newScriptedCollection(
		&scriptedBucket{values: map[string][]byte{}, scanErr: sentinel},
		&scriptedBucket{values: map[string][]byte{}},
		false,
	)

	err := collection.Scan(tx, nil, func(Key, string) (bool, error) { return true, nil })
	if !errors.Is(err, sentinel) {
		t.Fatalf("Scan error = %v, want %v", err, sentinel)
	}
}

func TestDecodeLengthRejectsBadCounters(t *testing.T) {
	if _, err := decodeLength([]byte("bad")); err == nil {
		t.Fatal("expected bad counter length error")
	}
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(int(^uint(0)>>1))+1)
	if _, err := decodeLength(raw[:]); err == nil {
		t.Fatal("expected counter overflow")
	}
}

func TestPutLengthRejectsInvalidAndStoreFailure(t *testing.T) {
	if err := putLength(&scriptedBucket{values: map[string][]byte{}}, Key("x"), -1); err == nil {
		t.Fatal("expected negative length error")
	}
	sentinel := errors.New("put failed")
	err := putLength(&scriptedBucket{values: map[string][]byte{}, putErr: sentinel}, Key("x"), 1)
	if !errors.Is(err, sentinel) {
		t.Fatalf("putLength error = %v, want %v", err, sentinel)
	}
}

func TestWrapTxnErrorReturnsSentinels(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, errReadOnly, ErrAtCapacity} {
		if got := wrapTxnError("op", err); !errors.Is(got, err) {
			t.Fatalf("wrapTxnError(%v) = %v", err, got)
		}
	}
}
