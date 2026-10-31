package rwibackfill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const checkpointFormat = "yago-rwi-backfill-v1"

var checkpointKey = vault.Key("current")

type checkpoint struct {
	Format              string `json:"format"`
	State               string `json:"state"`
	Cursor              string `json:"cursor"`
	ScannedDocuments    int64  `json:"scannedDocuments"`
	IndexedDocuments    int64  `json:"indexedDocuments"`
	SkippedDocuments    int64  `json:"skippedDocuments"`
	PostingRows         int64  `json:"postingRows"`
	EstimatedBytes      int64  `json:"estimatedBytes"`
	MaximumDocumentsRun int64  `json:"maximumDocumentsRun"`
	DryRun              bool   `json:"dryRun"`
	LastError           string `json:"lastError,omitempty"`
	UpdatedAtUnixMilli  int64  `json:"updatedAtUnixMilli"`
}

type checkpointCodec struct{}

func (checkpointCodec) Encode(value checkpoint) ([]byte, error) {
	if err := validateCheckpoint(value); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(value)
	return encoded, nil
}

func (checkpointCodec) Decode(encoded []byte) (checkpoint, error) {
	if len(encoded) == 0 || len(encoded) > 128<<10 {
		return checkpoint{}, errors.New("invalid backfill checkpoint size")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var value checkpoint
	if err := decoder.Decode(&value); err != nil {
		return checkpoint{}, fmt.Errorf("decode backfill checkpoint: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return checkpoint{}, errors.New("backfill checkpoint contains trailing data")
	}
	if err := validateCheckpoint(value); err != nil {
		return checkpoint{}, err
	}
	return value, nil
}

func validateCheckpoint(value checkpoint) error {
	if value.Format != checkpointFormat {
		return errors.New("unsupported backfill checkpoint format")
	}
	switch value.State {
	case stateRunning, statePauseRequested, statePaused, stateComplete, stateFailed:
	default:
		return errors.New("invalid backfill checkpoint state")
	}
	if value.ScannedDocuments < 0 || value.IndexedDocuments < 0 ||
		value.SkippedDocuments < 0 || value.PostingRows < 0 ||
		value.EstimatedBytes < 0 || value.MaximumDocumentsRun < 0 ||
		value.UpdatedAtUnixMilli <= 0 || len(value.Cursor) > 96<<10 ||
		len(value.LastError) > 512 {
		return errors.New("backfill checkpoint fields are outside supported bounds")
	}
	if value.IndexedDocuments+value.SkippedDocuments > value.ScannedDocuments {
		return errors.New("backfill checkpoint document totals are inconsistent")
	}
	return nil
}

type checkpointRepository struct {
	storage *vault.Vault
	records *vault.Collection[checkpoint]
}

func openCheckpointRepository(storage *vault.Vault) (*checkpointRepository, error) {
	records, err := vault.Register(
		storage,
		vault.Name("rwi_backfill_checkpoints"),
		checkpointCodec{},
	)
	if err != nil {
		return nil, fmt.Errorf("register rwi backfill checkpoints: %w", err)
	}
	return &checkpointRepository{storage: storage, records: records}, nil
}

func (r *checkpointRepository) load(ctx context.Context) (checkpoint, bool, error) {
	var value checkpoint
	found := false
	if err := r.storage.View(ctx, func(tx *vault.Txn) error {
		record, exists, err := r.records.Get(tx, checkpointKey)
		if err != nil {
			return fmt.Errorf("read checkpoint record: %w", err)
		}
		value, found = record, exists
		return nil
	}); err != nil {
		return checkpoint{}, false, fmt.Errorf("load rwi backfill checkpoint: %w", err)
	}
	return value, found, nil
}

func (r *checkpointRepository) save(ctx context.Context, value checkpoint) error {
	if err := validateCheckpoint(value); err != nil {
		return err
	}
	if err := r.storage.Update(ctx, func(tx *vault.Txn) error {
		return r.records.Put(tx, checkpointKey, value)
	}); err != nil {
		return fmt.Errorf("save rwi backfill checkpoint: %w", err)
	}
	return nil
}
