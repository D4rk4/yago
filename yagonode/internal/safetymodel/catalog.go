package safetymodel

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	catalogFormat        = "yago-content-safety-model-catalog-v1"
	maximumBytes         = 1 << 20
	maximumRevisionBytes = 128
)

const catalogBucket vault.Name = "content_safety_model"

var catalogKey = vault.Key("active")

type Snapshot struct {
	Revision string                       `json:"revision"`
	Model    contentsafety.CharacterModel `json:"model"`
}

func NewSnapshot(revision string, model contentsafety.CharacterModel) (Snapshot, error) {
	snapshot := Snapshot{Revision: revision, Model: model}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}

	return snapshot, nil
}

func (snapshot Snapshot) Validate() error {
	if !validRevision(snapshot.Revision) {
		return fmt.Errorf("content safety model revision is invalid")
	}
	if err := snapshot.Model.Validate(); err != nil {
		return fmt.Errorf("validate content safety model: %w", err)
	}

	return nil
}

type catalogRecord struct {
	Format   string    `json:"format"`
	Active   *Snapshot `json:"active,omitempty"`
	Previous *Snapshot `json:"previous,omitempty"`
}

type catalogCodec struct{}

func (catalogCodec) Encode(record catalogRecord) ([]byte, error) {
	if err := validateRecord(record); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(record)

	return encoded, nil
}

func (catalogCodec) Decode(encoded []byte) (catalogRecord, error) {
	if len(encoded) == 0 || len(encoded) > maximumBytes {
		return catalogRecord{}, fmt.Errorf("content safety model catalog size is invalid")
	}
	var record catalogRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return catalogRecord{}, fmt.Errorf("decode content safety model catalog: %w", err)
	}
	if err := validateRecord(record); err != nil {
		return catalogRecord{}, err
	}

	return cloneRecord(record), nil
}

type Status struct {
	Active           bool   `json:"active"`
	Revision         string `json:"revision,omitempty"`
	RollbackRevision string `json:"rollback_revision,omitempty"`
}

type Catalog struct {
	storage    *vault.Vault
	records    *vault.Collection[catalogRecord]
	changeLock sync.Mutex
	current    atomic.Pointer[Snapshot]
	record     catalogRecord
}

func Open(ctx context.Context, storage *vault.Vault) (*Catalog, error) {
	records, err := vault.Register(storage, catalogBucket, catalogCodec{})
	if err != nil {
		return nil, fmt.Errorf("register content safety model catalog: %w", err)
	}
	record := catalogRecord{Format: catalogFormat}
	if err := storage.View(ctx, func(tx *vault.Txn) error {
		stored, found, readErr := records.Get(tx, catalogKey)
		if readErr != nil {
			return fmt.Errorf("read content safety model catalog: %w", readErr)
		}
		if found {
			record = stored
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("load content safety model catalog: %w", err)
	}
	catalog := &Catalog{storage: storage, records: records, record: cloneRecord(record)}
	if record.Active != nil {
		active := *record.Active
		catalog.current.Store(&active)
	}

	return catalog, nil
}

func (catalog *Catalog) Classify(text string) contentsafety.Evidence {
	if catalog == nil {
		return contentsafety.Evidence{Rating: contentsafety.Unknown}
	}
	snapshot := catalog.current.Load()
	if snapshot == nil {
		return contentsafety.Evidence{Rating: contentsafety.Unknown}
	}

	return snapshot.Model.Classify(text)
}

func (catalog *Catalog) Status() Status {
	if catalog == nil {
		return Status{}
	}
	catalog.changeLock.Lock()
	defer catalog.changeLock.Unlock()
	status := Status{}
	if catalog.record.Active != nil {
		status.Active = true
		status.Revision = catalog.record.Active.Revision
	}
	if catalog.record.Previous != nil {
		status.RollbackRevision = catalog.record.Previous.Revision
	}

	return status
}

func (catalog *Catalog) Activate(ctx context.Context, snapshot Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	catalog.changeLock.Lock()
	defer catalog.changeLock.Unlock()
	next := catalogRecord{
		Format:   catalogFormat,
		Active:   cloneSnapshot(&snapshot),
		Previous: cloneSnapshot(catalog.record.Active),
	}
	if err := catalog.persist(ctx, next); err != nil {
		return err
	}
	active := snapshot
	catalog.current.Store(&active)
	catalog.record = next

	return nil
}

func (catalog *Catalog) Rollback(ctx context.Context) (bool, error) {
	catalog.changeLock.Lock()
	defer catalog.changeLock.Unlock()
	if catalog.record.Previous == nil {
		return false, nil
	}
	next := catalogRecord{
		Format:   catalogFormat,
		Active:   cloneSnapshot(catalog.record.Previous),
		Previous: cloneSnapshot(catalog.record.Active),
	}
	if err := catalog.persist(ctx, next); err != nil {
		return false, err
	}
	active := *next.Active
	catalog.current.Store(&active)
	catalog.record = next

	return true, nil
}

func (catalog *Catalog) ActiveSnapshotJSON() []byte {
	if catalog == nil {
		return nil
	}
	snapshot := catalog.current.Load()
	if snapshot == nil {
		return nil
	}
	encoded, _ := json.Marshal(snapshot)

	return encoded
}

func (catalog *Catalog) persist(ctx context.Context, record catalogRecord) error {
	if err := catalog.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := catalog.records.Put(tx, catalogKey, record); err != nil {
			return fmt.Errorf("write content safety model catalog: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist content safety model catalog: %w", err)
	}

	return nil
}

func validateRecord(record catalogRecord) error {
	if record.Format != catalogFormat {
		return fmt.Errorf("content safety model catalog format %q is unsupported", record.Format)
	}
	if record.Active == nil && record.Previous != nil {
		return fmt.Errorf("content safety rollback model has no active model")
	}
	for _, snapshot := range []*Snapshot{record.Active, record.Previous} {
		if snapshot != nil {
			if err := snapshot.Validate(); err != nil {
				return err
			}
		}
	}

	return nil
}

func cloneRecord(record catalogRecord) catalogRecord {
	return catalogRecord{
		Format:   record.Format,
		Active:   cloneSnapshot(record.Active),
		Previous: cloneSnapshot(record.Previous),
	}
}

func cloneSnapshot(snapshot *Snapshot) *Snapshot {
	if snapshot == nil {
		return nil
	}
	encoded, _ := json.Marshal(snapshot)
	var cloned Snapshot
	_ = json.Unmarshal(encoded, &cloned)

	return &cloned
}

func validRevision(revision string) bool {
	if len(revision) == 0 || len(revision) > maximumRevisionBytes {
		return false
	}
	for index, character := range []byte(revision) {
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		digit := character >= '0' && character <= '9'
		separator := character == '.' || character == '-' || character == '_'
		if index == 0 && !letter && !digit || index > 0 && !letter && !digit && !separator {
			return false
		}
	}

	return true
}
