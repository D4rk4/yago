package peerreputation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenStorageFailuresAndPersistedBounds(t *testing.T) {
	t.Parallel()
	configuration := DefaultConfiguration()

	malformedState, _ := newFaultVault(t)
	malformedState.provisionAndPut(stateKey, []byte(`{`))
	if _, err := Open(malformedState.vault, configuration); err == nil {
		t.Fatal("opened malformed state")
	}

	malformedPeer, _ := newFaultVault(t)
	malformedPeer.provisionAndPut(stateKey, encodeRecord(t, stateRecord(ledgerState{
		Configuration: configuration,
	})))
	malformedPeer.provisionAndPut(vault.Key("peer/bad"), []byte(`{`))
	if _, err := Open(malformedPeer.vault, configuration); err == nil {
		t.Fatal("opened malformed peer")
	}

	invalidKey, _ := newFaultVault(t)
	invalidKey.provisionAndPut(stateKey, encodeRecord(t, stateRecord(ledgerState{
		Configuration: configuration,
	})))
	invalidKey.provisionAndPut(vault.Key("peer/wrong"), encodeRecord(t, peerEntry(peerRecord{
		Peer: "actual", NetworkGroup: "group", LastObservedUnixNano: 1,
	})))
	if _, err := Open(invalidKey.vault, configuration); err == nil {
		t.Fatal("opened mismatched peer key")
	}

	boundedConfiguration := configuration
	boundedConfiguration.MaximumPeers = 1
	overfull, _ := newFaultVault(t)
	for _, identity := range []SignedPeerIdentity{"a", "b"} {
		overfull.provisionAndPut(peerKey(identity), encodeRecord(t, peerEntry(peerRecord{
			Peer: identity, NetworkGroup: "group", LastObservedUnixNano: 1,
		})))
	}
	if _, err := Open(overfull.vault, boundedConfiguration); err == nil {
		t.Fatal("opened overfull orphan records")
	}

	failedState, engine := newFaultVault(t)
	engine.failPut(stateKey)
	if _, err := Open(failedState.vault, configuration); err == nil {
		t.Fatal("opened after state write failure")
	}
}

func TestLedgerStorageFailures(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_800_000_000, 0).UTC()
	batch := ObservationBatch{Sequence: 1, Observations: []Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: OutcomeSuccess, ObservedAt: base,
	}}}
	configuration := DefaultConfiguration()

	invalidBatch, _ := newFaultLedger(t, configuration)
	if _, err := invalidBatch.ObserveBatch(context.Background(), ObservationBatch{}); err == nil {
		t.Fatal("accepted invalid batch")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := invalidBatch.ObserveBatch(canceled, batch); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled write, got %v", err)
	}

	malformedState, malformedStateEngine := newFaultLedger(t, configuration)
	malformedStateEngine.putRaw(stateKey, []byte(`{`))
	if _, err := malformedState.ObserveBatch(context.Background(), batch); err == nil {
		t.Fatal("observed with malformed state")
	}

	missingState, missingStateEngine := newFaultLedger(t, configuration)
	missingStateEngine.deleteRaw(stateKey)
	if _, err := missingState.ObserveBatch(context.Background(), batch); err == nil {
		t.Fatal("observed without state")
	}
	if _, err := missingState.LastBatchSequence(context.Background()); err == nil {
		t.Fatal("read sequence without state")
	}

	changedState, changedStateEngine := newFaultLedger(t, configuration)
	changed := configuration
	changed.HalfLife++
	changedStateEngine.putRaw(stateKey, encodeRecord(t, stateRecord(ledgerState{
		Configuration: changed,
	})))
	if _, err := changedState.ObserveBatch(context.Background(), batch); err == nil {
		t.Fatal("observed with changed configuration")
	}

	malformedPeer, malformedPeerEngine := newFaultLedger(t, configuration)
	malformedPeerEngine.putRaw(vault.Key("peer/bad"), []byte(`{`))
	if _, err := malformedPeer.ObserveBatch(context.Background(), batch); err == nil {
		t.Fatal("observed with malformed peer")
	}

	failedPeerWrite, failedPeerWriteEngine := newFaultLedger(t, configuration)
	failedPeerWriteEngine.failPut(peerKey("peer"))
	if _, err := failedPeerWrite.ObserveBatch(context.Background(), batch); err == nil {
		t.Fatal("observed after peer write failure")
	}

	failedStateWrite, failedStateWriteEngine := newFaultLedger(t, configuration)
	failedStateWriteEngine.failPut(stateKey)
	if _, err := failedStateWrite.ObserveBatch(context.Background(), batch); err == nil {
		t.Fatal("observed after batch state write failure")
	}

	failedEviction, failedEvictionEngine := newFaultLedger(t, configuration)
	if _, err := failedEviction.ObserveBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	failedEvictionEngine.failDelete(peerKey("peer"))
	if err := failedEviction.vault.Update(context.Background(), func(tx *vault.Txn) error {
		return failedEviction.writePeers(
			tx,
			map[SignedPeerIdentity]peerRecord{"peer": {
				Peer: "peer", NetworkGroup: "group", LastObservedUnixNano: base.UnixNano(),
			}},
			map[SignedPeerIdentity]peerRecord{},
		)
	}); err == nil {
		t.Fatal("evicted after delete failure")
	}
}

func TestSnapshotInputAndStorageFailures(t *testing.T) {
	t.Parallel()
	configuration := DefaultConfiguration()
	ledger, engine := newFaultLedger(t, configuration)
	if _, err := ledger.Snapshot(context.Background(), time.Time{}); err == nil {
		t.Fatal("accepted invalid snapshot time")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ledger.Snapshot(canceled, time.Unix(1, 0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled snapshot, got %v", err)
	}
	if _, err := ledger.LastBatchSequence(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled sequence read, got %v", err)
	}
	engine.putRaw(vault.Key("peer/bad"), []byte(`{`))
	if _, err := ledger.Snapshot(context.Background(), time.Unix(1, 0)); err == nil {
		t.Fatal("snapshotted malformed peer")
	}
}

func newFaultLedger(t *testing.T, configuration Configuration) (*ReputationLedger, *faultVault) {
	t.Helper()
	storage, engine := newFaultVault(t)
	ledger, err := Open(storage.vault, configuration)
	if err != nil {
		t.Fatal(err)
	}

	return ledger, engine
}

func encodeRecord(t *testing.T, record persistentRecord) []byte {
	t.Helper()
	encoded, err := (recordCodec{}).Encode(record)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

type faultVault struct {
	vault       *vault.Vault
	buckets     map[vault.Name]map[string][]byte
	putFailures map[string]struct{}
	delFailures map[string]struct{}
}

func newFaultVault(t *testing.T) (*faultVault, *faultVault) {
	t.Helper()
	engine := &faultVault{
		buckets:     map[vault.Name]map[string][]byte{},
		putFailures: map[string]struct{}{},
		delFailures: map[string]struct{}{},
	}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	engine.vault = storage
	t.Cleanup(func() { _ = storage.Close() })

	return engine, engine
}

func (engine *faultVault) Provision(name vault.Name) error {
	if engine.buckets[name] == nil {
		engine.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (engine *faultVault) Update(ctx context.Context, apply func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	staged := cloneFaultBuckets(engine.buckets)
	transaction := faultTransaction{engine: engine, buckets: staged, writable: true}
	if err := apply(transaction); err != nil {
		return err
	}
	engine.buckets = staged

	return nil
}

func (engine *faultVault) View(ctx context.Context, apply func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	return apply(faultTransaction{engine: engine, buckets: engine.buckets})
}

func (engine *faultVault) UsedBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("context: %w", err)
	}
	var used int64
	for _, bucket := range engine.buckets {
		for key, value := range bucket {
			used += int64(len(key) + len(value))
		}
	}

	return used, nil
}

func (engine *faultVault) QuotaBytes() int64 {
	return 0
}

func (engine *faultVault) Close() error {
	return nil
}

func (engine *faultVault) provisionAndPut(key vault.Key, value []byte) {
	_ = engine.Provision(recordBucket)
	engine.putRaw(key, value)
}

func (engine *faultVault) putRaw(key vault.Key, value []byte) {
	engine.buckets[recordBucket][string(key)] = append([]byte(nil), value...)
}

func (engine *faultVault) deleteRaw(key vault.Key) {
	delete(engine.buckets[recordBucket], string(key))
}

func (engine *faultVault) failPut(key vault.Key) {
	engine.putFailures[faultKey(recordBucket, key)] = struct{}{}
}

func (engine *faultVault) failDelete(key vault.Key) {
	engine.delFailures[faultKey(recordBucket, key)] = struct{}{}
}

type faultTransaction struct {
	engine   *faultVault
	buckets  map[vault.Name]map[string][]byte
	writable bool
}

func (transaction faultTransaction) Bucket(name vault.Name) vault.EngineBucket {
	return faultBucket{engine: transaction.engine, name: name, values: transaction.buckets[name]}
}

func (transaction faultTransaction) Writable() bool {
	return transaction.writable
}

type faultBucket struct {
	engine *faultVault
	name   vault.Name
	values map[string][]byte
}

func (bucket faultBucket) Get(key vault.Key) []byte {
	return bucket.values[string(key)]
}

func (bucket faultBucket) Put(key vault.Key, value []byte) error {
	if _, failed := bucket.engine.putFailures[faultKey(bucket.name, key)]; failed {
		return errors.New("injected put failure")
	}
	bucket.values[string(key)] = append([]byte(nil), value...)

	return nil
}

func (bucket faultBucket) Delete(key vault.Key) error {
	if _, failed := bucket.engine.delFailures[faultKey(bucket.name, key)]; failed {
		return errors.New("injected delete failure")
	}
	delete(bucket.values, string(key))

	return nil
}

func (bucket faultBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	keys := make([]string, 0, len(bucket.values))
	for key := range bucket.values {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		keep, err := visit(vault.Key(key), bucket.values[key])
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}

	return nil
}

func faultKey(name vault.Name, key vault.Key) string {
	return string(name) + "\x00" + string(key)
}

func cloneFaultBuckets(source map[vault.Name]map[string][]byte) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		values := make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			values[key] = append([]byte(nil), value...)
		}
		cloned[name] = values
	}

	return cloned
}
