package rankingmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	catalogFormat       = "yago-ranking-model-catalog-v1"
	catalogHistoryLimit = 8
	maximumCatalogBytes = learnedrank.MaximumSnapshotBytes*(catalogHistoryLimit+1) + 4096
)

const catalogBucket vault.Name = "ranking_models"

var catalogKey = vault.Key("active")

type catalogEntry struct {
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

type catalogRecord struct {
	Format  string         `json:"format"`
	Active  catalogEntry   `json:"active"`
	History []catalogEntry `json:"history"`
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
	if len(encoded) == 0 || len(encoded) > maximumCatalogBytes {
		return catalogRecord{}, fmt.Errorf("ranking model catalog size is invalid")
	}
	var record catalogRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return catalogRecord{}, fmt.Errorf("decode ranking model catalog: %w", err)
	}
	if err := validateRecord(record); err != nil {
		return catalogRecord{}, err
	}

	return cloneRecord(record), nil
}

type Revision struct {
	Active   bool                  `json:"active"`
	Revision string                `json:"revision,omitempty"`
	Kind     learnedrank.ModelKind `json:"model_kind,omitempty"`
}

type Status struct {
	Current  Revision   `json:"current"`
	Rollback []Revision `json:"rollback"`
}

type CatalogSnapshot struct {
	Status         Status          `json:"status"`
	ActiveSnapshot json.RawMessage `json:"active_snapshot,omitempty"`
}

type Catalog struct {
	storage    *vault.Vault
	records    *vault.Collection[catalogRecord]
	ranker     *learnedrank.Ranker
	changeLock sync.Mutex
	record     catalogRecord
}

func Open(
	ctx context.Context,
	storage *vault.Vault,
	candidateWindow int,
) (*Catalog, error) {
	ranker, err := learnedrank.NewRanker(candidateWindow)
	if err != nil {
		return nil, fmt.Errorf("create learned ranker: %w", err)
	}
	records, err := vault.Register(storage, catalogBucket, catalogCodec{})
	if err != nil {
		return nil, fmt.Errorf("register ranking model catalog: %w", err)
	}
	record := catalogRecord{Format: catalogFormat, History: []catalogEntry{}}
	if err := storage.View(ctx, func(tx *vault.Txn) error {
		stored, exists, readErr := records.Get(tx, catalogKey)
		if readErr != nil {
			return fmt.Errorf("read ranking model catalog: %w", readErr)
		}
		if exists {
			record = stored
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("load ranking model catalog: %w", err)
	}
	restoreRanker(ranker, record)

	return &Catalog{
		storage: storage,
		records: records,
		ranker:  ranker,
		record:  cloneRecord(record),
	}, nil
}

func (c *Catalog) Ranker() *learnedrank.Ranker {
	if c == nil {
		return nil
	}

	return c.ranker
}

func (c *Catalog) Snapshot() CatalogSnapshot {
	if c == nil {
		return CatalogSnapshot{Status: Status{Rollback: []Revision{}}}
	}
	c.changeLock.Lock()
	defer c.changeLock.Unlock()
	status := Status{
		Current:  revisionForEntry(c.record.Active),
		Rollback: make([]Revision, len(c.record.History)),
	}
	for index, entry := range c.record.History {
		status.Rollback[len(status.Rollback)-index-1] = revisionForEntry(entry)
	}

	return CatalogSnapshot{
		Status:         status,
		ActiveSnapshot: append(json.RawMessage(nil), c.record.Active.Snapshot...),
	}
}

func (c *Catalog) Activate(ctx context.Context, snapshot learnedrank.Snapshot) error {
	_, err := c.activate(ctx, nil, false, snapshot)

	return err
}

func (c *Catalog) ActivateIfCurrent(
	ctx context.Context,
	incumbent []byte,
	snapshot learnedrank.Snapshot,
) (bool, error) {
	return c.activate(ctx, incumbent, true, snapshot)
}

func (c *Catalog) activate(
	ctx context.Context,
	incumbent []byte,
	checkIncumbent bool,
	snapshot learnedrank.Snapshot,
) (bool, error) {
	encoded, err := snapshot.MarshalJSON()
	if err != nil {
		return false, fmt.Errorf("encode ranking model activation: %w", err)
	}
	c.changeLock.Lock()
	defer c.changeLock.Unlock()
	if checkIncumbent && !bytes.Equal(incumbent, c.record.Active.Snapshot) {
		return false, nil
	}
	next := cloneRecord(c.record)
	next.History = append(next.History, cloneEntry(next.Active))
	if len(next.History) > catalogHistoryLimit {
		next.History = append(
			[]catalogEntry(nil),
			next.History[len(next.History)-catalogHistoryLimit:]...)
	}
	next.Active = catalogEntry{Snapshot: append(json.RawMessage(nil), encoded...)}
	if err := c.persist(ctx, next); err != nil {
		return false, err
	}
	_ = c.ranker.Activate(snapshot)
	c.record = next

	return true, nil
}

func (c *Catalog) Rollback(ctx context.Context) (bool, error) {
	c.changeLock.Lock()
	defer c.changeLock.Unlock()
	if len(c.record.History) == 0 {
		return false, nil
	}
	next := cloneRecord(c.record)
	last := len(next.History) - 1
	next.Active = cloneEntry(next.History[last])
	next.History = next.History[:last]
	if err := c.persist(ctx, next); err != nil {
		return false, err
	}
	_ = c.ranker.Rollback()
	c.record = next

	return true, nil
}

func (c *Catalog) persist(ctx context.Context, record catalogRecord) error {
	if err := c.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := c.records.Put(tx, catalogKey, record); err != nil {
			return fmt.Errorf("write ranking model catalog: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist ranking model catalog: %w", err)
	}

	return nil
}

func restoreRanker(ranker *learnedrank.Ranker, record catalogRecord) {
	for _, entry := range record.History {
		if snapshot, active := snapshotForEntry(entry); active {
			_ = ranker.Activate(snapshot)
		}
	}
	if snapshot, active := snapshotForEntry(record.Active); active {
		_ = ranker.Activate(snapshot)
	}
}

func validateRecord(record catalogRecord) error {
	if record.Format != catalogFormat {
		return fmt.Errorf("ranking model catalog format %q is unsupported", record.Format)
	}
	if len(record.History) > catalogHistoryLimit {
		return fmt.Errorf("ranking model catalog history exceeds its limit")
	}
	if _, _, err := validateEntry(record.Active); err != nil {
		return fmt.Errorf("validate active ranking model: %w", err)
	}
	for index, entry := range record.History {
		active, _, err := validateEntry(entry)
		if err != nil {
			return fmt.Errorf("validate ranking model history entry %d: %w", index, err)
		}
		if !active && index != 0 {
			return fmt.Errorf("inactive ranking model history entry must be first")
		}
	}
	if len(record.Active.Snapshot) == 0 && len(record.History) != 0 {
		return fmt.Errorf("inactive ranking model catalog cannot have rollback history")
	}

	return nil
}

func validateEntry(entry catalogEntry) (bool, learnedrank.Snapshot, error) {
	if len(entry.Snapshot) == 0 {
		return false, learnedrank.Snapshot{}, nil
	}
	snapshot, err := learnedrank.ParseSnapshot(entry.Snapshot)
	if err != nil {
		return false, learnedrank.Snapshot{}, fmt.Errorf("parse ranking model snapshot: %w", err)
	}

	return true, snapshot, nil
}

func snapshotForEntry(entry catalogEntry) (learnedrank.Snapshot, bool) {
	active, snapshot, _ := validateEntry(entry)

	return snapshot, active
}

func revisionForEntry(entry catalogEntry) Revision {
	active, snapshot, _ := validateEntry(entry)
	if !active {
		return Revision{}
	}

	return Revision{Active: true, Revision: snapshot.Revision(), Kind: snapshot.Kind()}
}

func cloneRecord(record catalogRecord) catalogRecord {
	cloned := catalogRecord{
		Format:  record.Format,
		Active:  cloneEntry(record.Active),
		History: make([]catalogEntry, len(record.History)),
	}
	for index, entry := range record.History {
		cloned.History[index] = cloneEntry(entry)
	}

	return cloned
}

func cloneEntry(entry catalogEntry) catalogEntry {
	return catalogEntry{Snapshot: append(json.RawMessage(nil), entry.Snapshot...)}
}
