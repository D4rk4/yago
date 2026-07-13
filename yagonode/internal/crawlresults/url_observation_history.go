package crawlresults

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const urlObservationBucket vault.Name = "crawl_url_observations"

type observationDisposition uint8

const (
	observationApply observationDisposition = iota
	observationDuplicate
	observationSuperseded
)

type urlObservationRecord struct {
	ObservedAt    time.Time
	ObservationID string
}

type urlObservationCodec struct{}

func (urlObservationCodec) Encode(record urlObservationRecord) ([]byte, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal URL observation: %w", err)
	}

	return data, nil
}

func (urlObservationCodec) Decode(raw []byte) (urlObservationRecord, error) {
	var record urlObservationRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return urlObservationRecord{}, fmt.Errorf("unmarshal URL observation: %w", err)
	}

	return record, nil
}

type observationHistory interface {
	Begin(context.Context, []yagocrawlcontract.IngestBatch) ([]observationDisposition, error)
	Complete(context.Context, []yagocrawlcontract.IngestBatch) error
}

type URLObservationHistory struct {
	vault   *vault.Vault
	records *vault.Collection[urlObservationRecord]
}

func OpenURLObservationHistory(v *vault.Vault) (*URLObservationHistory, error) {
	records, err := vault.Register(v, urlObservationBucket, urlObservationCodec{})
	if err != nil {
		return nil, fmt.Errorf("register URL observation history: %w", err)
	}

	return &URLObservationHistory{vault: v, records: records}, nil
}

func (h *URLObservationHistory) Begin(
	ctx context.Context,
	batches []yagocrawlcontract.IngestBatch,
) ([]observationDisposition, error) {
	dispositions := make([]observationDisposition, len(batches))
	err := h.vault.View(ctx, func(tx *vault.Txn) error {
		clear(dispositions)
		for index, batch := range batches {
			candidate, err := observationRecord(batch)
			if err != nil {
				return err
			}
			key := observationURLKey(batch.SourceURL)
			current, found, err := h.records.Get(tx, key)
			if err != nil {
				return fmt.Errorf("read URL observation %q: %w", batch.SourceURL, err)
			}
			if !found {
				continue
			}
			switch compareObservations(candidate, current) {
			case -1:
				dispositions[index] = observationSuperseded
			case 0:
				dispositions[index] = observationDuplicate
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("begin URL observations: %w", err)
	}

	return dispositions, nil
}

func (h *URLObservationHistory) Complete(
	ctx context.Context,
	batches []yagocrawlcontract.IngestBatch,
) error {
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, batch := range batches {
			if err := h.completeOne(tx, batch); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("complete URL observations: %w", err)
	}

	return nil
}

func (h *URLObservationHistory) completeOne(
	tx *vault.Txn,
	batch yagocrawlcontract.IngestBatch,
) error {
	candidate, err := observationRecord(batch)
	if err != nil {
		return err
	}
	key := observationURLKey(batch.SourceURL)
	current, found, err := h.records.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read URL observation %q: %w", batch.SourceURL, err)
	}
	if found {
		comparison := compareObservations(candidate, current)
		if comparison < 0 {
			return fmt.Errorf("URL observation %q is no longer current", batch.SourceURL)
		}
		if comparison == 0 {
			return nil
		}
	}
	if err := h.records.Put(tx, key, candidate); err != nil {
		return fmt.Errorf("complete URL observation %q: %w", batch.SourceURL, err)
	}

	return nil
}

func observationRecord(batch yagocrawlcontract.IngestBatch) (urlObservationRecord, error) {
	observedAt := batch.ObservedAt
	if observedAt.IsZero() {
		observedAt = batch.Document.FetchedAt
	}
	if !observedAt.IsZero() {
		observedAt = observedAt.UTC()
	}
	observationID := batch.ObservationID
	if observationID == "" {
		data, err := yagocrawlcontract.MarshalIngestBatch(batch)
		if err != nil {
			return urlObservationRecord{}, fmt.Errorf("identify URL observation: %w", err)
		}
		identity := sha256.Sum256(data)
		observationID = hex.EncodeToString(identity[:])
	}

	return urlObservationRecord{
		ObservedAt:    observedAt,
		ObservationID: observationID,
	}, nil
}

func compareObservations(candidate, current urlObservationRecord) int {
	if candidate.ObservedAt.Before(current.ObservedAt) {
		return -1
	}
	if candidate.ObservedAt.After(current.ObservedAt) {
		return 1
	}
	if candidate.ObservationID < current.ObservationID {
		return -1
	}
	if candidate.ObservationID > current.ObservationID {
		return 1
	}

	return 0
}

func observationURLKey(sourceURL string) vault.Key {
	identity := sha256.Sum256([]byte(sourceURL))

	return vault.Key(identity[:])
}
