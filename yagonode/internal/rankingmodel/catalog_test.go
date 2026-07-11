package rankingmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type rawCodec struct{}

func (rawCodec) Encode(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

func (rawCodec) Decode(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

type failingEngine struct{}

func (failingEngine) Update(_ context.Context, operation func(vault.EngineTxn) error) error {
	return operation(failingTransaction{})
}

func (failingEngine) View(context.Context, func(vault.EngineTxn) error) error { return nil }
func (failingEngine) Provision(vault.Name) error                              { return nil }
func (failingEngine) UsedBytes(context.Context) (int64, error)                { return 0, nil }
func (failingEngine) QuotaBytes() int64                                       { return 0 }
func (failingEngine) Close() error                                            { return nil }

type failingTransaction struct{}

func (failingTransaction) Bucket(vault.Name) vault.EngineBucket { return failingBucket{} }
func (failingTransaction) Writable() bool                       { return true }

type failingBucket struct{}

func (failingBucket) Get(vault.Key) []byte { return nil }
func (failingBucket) Put(vault.Key, []byte) error {
	return errors.New("write failed")
}
func (failingBucket) Delete(vault.Key) error { return nil }
func (failingBucket) Scan(vault.Key, func(vault.Key, []byte) (bool, error)) error {
	return nil
}

func TestCatalogActivatesCapsHistoryAndRollsBack(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage, learnedrank.DefaultCandidateWindow)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if catalog.Ranker() == nil || catalog.Snapshot().Status.Current.Active ||
		catalog.Snapshot().ActiveSnapshot != nil {
		t.Fatalf("empty catalog = %#v", catalog.Snapshot())
	}
	for revision := 1; revision <= 10; revision++ {
		if err := catalog.Activate(
			t.Context(),
			linearSnapshot(t, fmt.Sprintf("v%d", revision), float64(revision)),
		); err != nil {
			t.Fatalf("Activate %d: %v", revision, err)
		}
	}
	status := catalog.Snapshot().Status
	if status.Current.Revision != "v10" ||
		status.Current.Kind != learnedrank.ModelLinearLambdaRank ||
		len(status.Rollback) != catalogHistoryLimit ||
		status.Rollback[0].Revision != "v9" ||
		status.Rollback[7].Revision != "v2" {
		t.Fatalf("status = %#v", status)
	}
	encoded := catalog.Snapshot().ActiveSnapshot
	encoded[0] = '!'
	if catalog.Snapshot().ActiveSnapshot[0] == '!' {
		t.Fatal("active snapshot JSON was not copied")
	}
	rolledBack, err := catalog.Rollback(t.Context())
	if err != nil || !rolledBack || catalog.Snapshot().Status.Current.Revision != "v9" {
		t.Fatalf("Rollback = %v, %v, %#v", rolledBack, err, catalog.Snapshot())
	}
}

func TestCatalogPersistsActiveModelAndRollbackState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage, 32)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, revision := range []string{"first", "second"} {
		if err := catalog.Activate(t.Context(), linearSnapshot(t, revision, 1)); err != nil {
			t.Fatalf("Activate %s: %v", revision, err)
		}
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault.Open reopened: %v", err)
	}
	restored, err := Open(t.Context(), reopened, 32)
	if err != nil {
		t.Fatalf("Open restored: %v", err)
	}
	active, exists := restored.Ranker().ActiveSnapshot()
	if !exists || active.Revision() != "second" ||
		restored.Snapshot().Status.Rollback[0].Revision != "first" {
		t.Fatalf("restored status = %#v, %#v, %v", restored.Snapshot(), active, exists)
	}
	if rolledBack, rollbackErr := restored.Rollback(t.Context()); rollbackErr != nil ||
		!rolledBack || restored.Snapshot().Status.Current.Revision != "first" {
		t.Fatalf("restored rollback = %v, %v, %#v", rolledBack, rollbackErr, restored.Snapshot())
	}
	if rolledBack, rollbackErr := restored.Rollback(t.Context()); rollbackErr != nil ||
		!rolledBack || restored.Snapshot().Status.Current.Active {
		t.Fatalf("inactive rollback = %v, %v, %#v", rolledBack, rollbackErr, restored.Snapshot())
	}
	if rolledBack, rollbackErr := restored.Rollback(t.Context()); rollbackErr != nil || rolledBack {
		t.Fatalf("empty rollback = %v, %v", rolledBack, rollbackErr)
	}
}

func TestCatalogRejectsInvalidOperationsWithoutChangingActiveModel(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage, 8)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := catalog.Activate(t.Context(), learnedrank.Snapshot{}); err == nil {
		t.Fatal("invalid snapshot was activated")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := catalog.Activate(canceled, linearSnapshot(t, "canceled", 1)); err == nil ||
		catalog.Snapshot().Status.Current.Active {
		t.Fatalf("canceled activation = %v, %#v", err, catalog.Snapshot())
	}
	if err := catalog.Activate(t.Context(), linearSnapshot(t, "active", 1)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if rolledBack, rollbackErr := catalog.Rollback(canceled); rollbackErr == nil || rolledBack ||
		catalog.Snapshot().Status.Current.Revision != "active" {
		t.Fatalf("canceled rollback = %v, %v, %#v", rolledBack, rollbackErr, catalog.Snapshot())
	}
}

func TestCatalogActivationComparesAtomicIncumbentSnapshot(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage, 8)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := catalog.Activate(t.Context(), linearSnapshot(t, "first", 1)); err != nil {
		t.Fatalf("Activate first: %v", err)
	}
	view := catalog.Snapshot()
	activated, err := catalog.ActivateIfCurrent(
		t.Context(),
		[]byte("stale"),
		linearSnapshot(t, "rejected", 1),
	)
	if err != nil || activated || catalog.Snapshot().Status.Current.Revision != "first" {
		t.Fatalf("stale activation = %v, %v, %+v", activated, err, catalog.Snapshot())
	}
	activated, err = catalog.ActivateIfCurrent(
		t.Context(),
		view.ActiveSnapshot,
		linearSnapshot(t, "second", 1),
	)
	if err != nil || !activated || catalog.Snapshot().Status.Current.Revision != "second" {
		t.Fatalf("matched activation = %v, %v, %+v", activated, err, catalog.Snapshot())
	}
}

func TestCatalogSnapshotStaysCoherentDuringActivation(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage, 8)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := catalog.Activate(t.Context(), linearSnapshot(t, "initial", 1)); err != nil {
		t.Fatalf("Activate initial: %v", err)
	}
	snapshots := make([]learnedrank.Snapshot, 20)
	for revision := range snapshots {
		snapshots[revision] = linearSnapshot(t, fmt.Sprintf("revision-%d", revision), 1)
	}
	ctx := t.Context()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for _, snapshot := range snapshots {
			if err := catalog.Activate(ctx, snapshot); err != nil {
				t.Errorf("Activate: %v", err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for range 100 {
			view := catalog.Snapshot()
			snapshot, err := learnedrank.ParseSnapshot(view.ActiveSnapshot)
			if err != nil || snapshot.Revision() != view.Status.Current.Revision {
				t.Errorf("incoherent view = %+v, %+v, %v", view, snapshot, err)
				return
			}
		}
	}()
	workers.Wait()
}

func TestCatalogOpenAndStorageFailures(t *testing.T) {
	if _, err := Open(t.Context(), nil, 1); err == nil {
		t.Fatal("invalid candidate window was accepted")
	}
	closed, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Open(t.Context(), closed, 8); err == nil {
		t.Fatal("closed vault was accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if _, err := Open(canceled, storage, 8); err == nil {
		t.Fatal("canceled open was accepted")
	}
	failing, err := vault.New(failingEngine{})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	catalog, err := Open(t.Context(), failing, 8)
	if err != nil {
		t.Fatalf("Open failing engine: %v", err)
	}
	if err := catalog.Activate(t.Context(), linearSnapshot(t, "write", 1)); err == nil {
		t.Fatal("failed catalog write was accepted")
	}
}

func TestCatalogOpenRejectsCorruptPersistedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault.Open: %v", err)
	}
	raw, err := vault.Register(storage, catalogBucket, rawCodec{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return raw.Put(tx, catalogKey, []byte("not-json"))
	}); err != nil {
		t.Fatalf("seed corrupt catalog: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault.Open reopened: %v", err)
	}
	if _, err := Open(t.Context(), reopened, 8); err == nil {
		t.Fatal("corrupt catalog was accepted")
	}
}

func TestCatalogCodecValidationAndCopies(t *testing.T) {
	codec := catalogCodec{}
	valid := catalogRecord{Format: catalogFormat, History: []catalogEntry{}}
	encoded, err := codec.Encode(valid)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("Decode = %#v, %v", decoded, err)
	}
	snapshot := encodedSnapshot(t, "valid")
	invalid := []catalogRecord{
		{},
		{Format: catalogFormat, History: make([]catalogEntry, catalogHistoryLimit+1)},
		{Format: catalogFormat, Active: catalogEntry{Snapshot: json.RawMessage(`{}`)}},
		{
			Format:  catalogFormat,
			Active:  catalogEntry{Snapshot: snapshot},
			History: []catalogEntry{{Snapshot: json.RawMessage(`{}`)}},
		},
		{
			Format:  catalogFormat,
			Active:  catalogEntry{Snapshot: snapshot},
			History: []catalogEntry{{}, {}},
		},
		{Format: catalogFormat, History: []catalogEntry{{}}},
	}
	for index, record := range invalid {
		if _, err := codec.Encode(record); err == nil {
			t.Fatalf("invalid record %d was encoded", index)
		}
	}
	for index, raw := range [][]byte{
		nil,
		make([]byte, maximumCatalogBytes+1),
		[]byte("{"),
		[]byte(`{"format":"future","active":{},"history":[]}`),
	} {
		if _, err := codec.Decode(raw); err == nil {
			t.Fatalf("invalid bytes %d were decoded", index)
		}
	}
	record := catalogRecord{
		Format:  catalogFormat,
		Active:  catalogEntry{Snapshot: snapshot},
		History: []catalogEntry{{}, {Snapshot: snapshot}},
	}
	cloned := cloneRecord(record)
	cloned.Active.Snapshot[0] = '!'
	cloned.History[1].Snapshot[0] = '!'
	if record.Active.Snapshot[0] == '!' || record.History[1].Snapshot[0] == '!' {
		t.Fatal("catalog record was not deeply cloned")
	}
	if active, _, err := validateEntry(catalogEntry{}); err != nil || active {
		t.Fatalf("inactive entry = %v, %v", active, err)
	}
}

func TestNilCatalogAccessors(t *testing.T) {
	var catalog *Catalog
	if catalog.Ranker() != nil || catalog.Snapshot().ActiveSnapshot != nil ||
		catalog.Snapshot().Status.Current.Active || catalog.Snapshot().Status.Rollback == nil {
		t.Fatalf("nil catalog accessors = %#v", catalog.Snapshot())
	}
}

func linearSnapshot(t *testing.T, revision string, weight float64) learnedrank.Snapshot {
	t.Helper()
	definitions := learnedrank.FeatureDefinitions()
	weights := make([]float64, len(definitions))
	weights[0] = weight
	model, err := rankfit.NewLinearLambdaRankModel(definitions, weights)
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel: %v", err)
	}
	snapshot, err := learnedrank.NewLinearSnapshot(revision, model)
	if err != nil {
		t.Fatalf("NewLinearSnapshot: %v", err)
	}

	return snapshot
}

func encodedSnapshot(t *testing.T, revision string) json.RawMessage {
	t.Helper()
	encoded, err := linearSnapshot(t, revision, 1).MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	return append(json.RawMessage(nil), encoded...)
}

func TestCatalogJSONShapeIsBounded(t *testing.T) {
	encoded, err := json.Marshal(Status{
		Current:  Revision{Active: true, Revision: "v1", Kind: learnedrank.ModelLinearLambdaRank},
		Rollback: []Revision{},
	})
	if err != nil || !strings.Contains(string(encoded), `"revision":"v1"`) {
		t.Fatalf("status JSON = %s, %v", encoded, err)
	}
}
