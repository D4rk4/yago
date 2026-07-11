package safetymodel

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
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

func TestCatalogActivatesClassifiesAndRollsBack(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if catalog.Status().Active || catalog.Classify("text").Rating != contentsafety.Unknown ||
		catalog.ActiveSnapshotJSON() != nil {
		t.Fatalf("empty catalog = %#v", catalog.Status())
	}
	model := trainedModel(t)
	first, err := NewSnapshot("first", model)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	if err := catalog.Activate(t.Context(), first); err != nil {
		t.Fatalf("Activate first: %v", err)
	}
	if evidence := catalog.Classify(
		"family archive public guide",
	); evidence.Rating == contentsafety.Unknown {
		t.Fatalf("classified evidence = %#v", evidence)
	}
	second, err := NewSnapshot("second", model)
	if err != nil {
		t.Fatalf("NewSnapshot second: %v", err)
	}
	if err := catalog.Activate(t.Context(), second); err != nil {
		t.Fatalf("Activate second: %v", err)
	}
	status := catalog.Status()
	if !status.Active || status.Revision != "second" || status.RollbackRevision != "first" ||
		!strings.Contains(string(catalog.ActiveSnapshotJSON()), `"revision":"second"`) {
		t.Fatalf("status = %#v", status)
	}
	rolledBack, err := catalog.Rollback(t.Context())
	if err != nil || !rolledBack || catalog.Status().Revision != "first" ||
		catalog.Status().RollbackRevision != "second" {
		t.Fatalf("Rollback = %v, %v, %#v", rolledBack, err, catalog.Status())
	}
}

func TestCatalogRollbackWithoutPreviousAndCanceledChanges(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rolledBack, rollbackErr := catalog.Rollback(t.Context()); rollbackErr != nil || rolledBack {
		t.Fatalf("empty rollback = %v, %v", rolledBack, rollbackErr)
	}
	if err := catalog.Activate(t.Context(), Snapshot{}); err == nil {
		t.Fatal("invalid snapshot was activated")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	snapshot, err := NewSnapshot("model", trainedModel(t))
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	if err := catalog.Activate(canceled, snapshot); err == nil || catalog.Status().Active {
		t.Fatalf("canceled activation = %v, %#v", err, catalog.Status())
	}
	if err := catalog.Activate(t.Context(), snapshot); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	second, err := NewSnapshot("second", trainedModel(t))
	if err != nil {
		t.Fatalf("NewSnapshot second: %v", err)
	}
	if err := catalog.Activate(t.Context(), second); err != nil {
		t.Fatalf("Activate second: %v", err)
	}
	if rolledBack, rollbackErr := catalog.Rollback(canceled); rollbackErr == nil || rolledBack ||
		catalog.Status().Revision != "second" {
		t.Fatalf("canceled rollback = %v, %v, %#v", rolledBack, rollbackErr, catalog.Status())
	}
}

func TestCatalogPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "safety.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault.Open: %v", err)
	}
	catalog, err := Open(t.Context(), storage)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	snapshot, err := NewSnapshot("persisted", trainedModel(t))
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	if err := catalog.Activate(t.Context(), snapshot); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault.Open reopened: %v", err)
	}
	restored, err := Open(t.Context(), reopened)
	if err != nil || restored.Status().Revision != "persisted" ||
		restored.Classify("public family archive").Rating == contentsafety.Unknown {
		t.Fatalf("restored = %#v, %v", restored.Status(), err)
	}
}

func TestSnapshotAndCatalogValidation(t *testing.T) {
	model := trainedModel(t)
	for _, revision := range []string{"", ".bad", "bad/value", strings.Repeat("a", 129)} {
		if _, err := NewSnapshot(revision, model); err == nil {
			t.Fatalf("revision %q was accepted", revision)
		}
	}
	if _, err := NewSnapshot("invalid-model", contentsafety.CharacterModel{}); err == nil {
		t.Fatal("invalid model was accepted")
	}
	if !validRevision("9.model-v_1") {
		t.Fatal("valid revision was rejected")
	}
	codec := catalogCodec{}
	active, err := NewSnapshot("active", model)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	valid := catalogRecord{Format: catalogFormat, Active: &active}
	encoded, err := codec.Encode(valid)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("Decode = %#v, %v", decoded, err)
	}
	invalidRecords := []catalogRecord{
		{},
		{Format: catalogFormat, Previous: &active},
		{Format: catalogFormat, Active: &Snapshot{}},
	}
	for index, record := range invalidRecords {
		if _, err := codec.Encode(record); err == nil {
			t.Fatalf("invalid record %d was encoded", index)
		}
	}
	for index, raw := range [][]byte{
		nil,
		make([]byte, maximumBytes+1),
		[]byte("{"),
		[]byte(`{"format":"future"}`),
	} {
		if _, err := codec.Decode(raw); err == nil {
			t.Fatalf("invalid bytes %d were decoded", index)
		}
	}
	cloned := cloneRecord(valid)
	cloned.Active.Revision = "changed"
	if valid.Active.Revision == "changed" || cloneSnapshot(nil) != nil {
		t.Fatal("catalog values were not cloned")
	}
}

func TestCatalogOpenAndWriteFailures(t *testing.T) {
	closed, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Open(t.Context(), closed); err == nil {
		t.Fatal("closed vault was accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	if _, err := Open(canceled, storage); err == nil {
		t.Fatal("canceled open was accepted")
	}
	failing, err := vault.New(failingEngine{})
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	catalog, err := Open(t.Context(), failing)
	if err != nil {
		t.Fatalf("Open failing engine: %v", err)
	}
	snapshot, err := NewSnapshot("write", trainedModel(t))
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	if err := catalog.Activate(t.Context(), snapshot); err == nil {
		t.Fatal("failed write was accepted")
	}
}

func TestCatalogOpenRejectsCorruptPersistence(t *testing.T) {
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
		return raw.Put(tx, catalogKey, []byte("bad"))
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
	if _, err := Open(t.Context(), reopened); err == nil {
		t.Fatal("corrupt catalog was accepted")
	}
}

func TestNilCatalog(t *testing.T) {
	var catalog *Catalog
	if catalog.Classify("text").Rating != contentsafety.Unknown || catalog.Status().Active ||
		catalog.ActiveSnapshotJSON() != nil {
		t.Fatal("nil catalog was not neutral")
	}
}

func trainedModel(t *testing.T) contentsafety.CharacterModel {
	t.Helper()
	model, err := contentsafety.TrainCharacterModel(t.Context(), []contentsafety.LabeledDocument{
		{Text: "family archive catalogue calm public alpha", Rating: contentsafety.General},
		{Text: "family guide library calm public beta", Rating: contentsafety.General},
		{Text: "family reference collection calm public gamma", Rating: contentsafety.General},
		{Text: "restricted mature section private lambda", Rating: contentsafety.Explicit},
		{Text: "restricted private catalogue mature omega", Rating: contentsafety.Explicit},
		{Text: "restricted archive private mature sigma", Rating: contentsafety.Explicit},
	})
	if err != nil {
		t.Fatalf("TrainCharacterModel: %v", err)
	}

	return model
}
