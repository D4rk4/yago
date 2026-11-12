package searchdemand

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	updateIndexName    vault.Name = "search_demand_updates"
	updateSequenceName vault.Name = "search_demand_sequence"
	updateBatchSize               = 512
)

var (
	lastUpdateSequenceKey = vault.Key("last")
	updateIndexReadyKey   = vault.Key("ready")
)

type stringCodec struct{}

func (stringCodec) Encode(value string) ([]byte, error) {
	return []byte(value), nil
}

func (stringCodec) Decode(value []byte) (string, error) {
	return string(value), nil
}

type sequenceCodec struct{}

func (sequenceCodec) Encode(value uint64) ([]byte, error) {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded, nil
}

func (sequenceCodec) Decode(value []byte) (uint64, error) {
	if len(value) != 8 {
		return 0, errors.New("invalid search demand sequence")
	}
	return binary.BigEndian.Uint64(value), nil
}

func (store *Store) recordUpdate(tx *vault.Txn, key vault.Key, record Record) error {
	last, found, err := store.sequence.Get(tx, lastUpdateSequenceKey)
	if err != nil {
		return err
	}
	if !found {
		last = 0
	}
	if last == math.MaxUint64 {
		return errors.New("search demand sequence exhausted")
	}
	if record.UpdateSequence > 0 {
		if _, err := store.updates.Delete(tx, sequenceKey(record.UpdateSequence)); err != nil {
			return err
		}
	}
	last++
	record.UpdateSequence = last
	if err := store.records.Put(tx, key, record); err != nil {
		return err
	}
	if err := store.updates.Put(tx, sequenceKey(last), string(key)); err != nil {
		return err
	}
	return store.sequence.Put(tx, lastUpdateSequenceKey, last)
}

func (store *Store) initializeUpdateIndex(ctx context.Context) error {
	var ready bool
	if err := store.vault.View(ctx, func(tx *vault.Txn) error {
		value, found, err := store.sequence.Get(tx, updateIndexReadyKey)
		ready = found && value == 1
		return err
	}); err != nil {
		return err
	}
	if ready {
		return nil
	}
	cursor := ""
	for {
		page, err := store.ReadPage(ctx, cursor, updateBatchSize)
		if err != nil {
			return err
		}
		if len(page.Records) == 0 {
			break
		}
		if err := store.vault.Update(ctx, func(tx *vault.Txn) error {
			for _, record := range page.Records {
				if record.UpdateSequence != 0 {
					continue
				}
				key := demandKey(record.Topic, record.URL)
				current, found, err := store.records.Get(tx, key)
				if err != nil {
					return err
				}
				if !found || current.UpdateSequence != 0 {
					continue
				}
				if err := store.recordUpdate(tx, key, current); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	return store.vault.Update(ctx, func(tx *vault.Txn) error {
		return store.sequence.Put(tx, updateIndexReadyKey, 1)
	})
}

func (store *Store) ReadUpdates(ctx context.Context, cursor string, pageSize int) (Page, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaximumPageSize {
		return Page{}, fmt.Errorf("search demand update page size outside range: %d", pageSize)
	}
	var after uint64
	if cursor != "" {
		parsed, err := strconv.ParseUint(cursor, 10, 64)
		if err != nil || strconv.FormatUint(parsed, 10) != cursor {
			return Page{}, errors.New("invalid search demand update cursor")
		}
		after = parsed
	}
	page := Page{Records: make([]Record, 0, pageSize)}
	if err := store.vault.View(ctx, func(tx *vault.Txn) error {
		last, _, err := store.sequence.Get(tx, lastUpdateSequenceKey)
		if err != nil {
			return err
		}
		if after > last {
			after = 0
			page.NextCursor = "0"
		} else {
			page.NextCursor = cursor
		}
		var afterKey vault.Key
		if after > 0 {
			afterKey = sequenceKey(after)
		}
		batch, err := tx.ReadBucketPage(updateIndexName, afterKey, pageSize+1)
		if err != nil {
			return err
		}
		if len(batch.Entries) > pageSize {
			batch.Entries = batch.Entries[:pageSize]
		}
		for _, entry := range batch.Entries {
			sequence, err := sequenceFromKey(entry.Key)
			if err != nil {
				return err
			}
			record, found, err := store.records.Get(tx, vault.Key(entry.Value))
			if err != nil {
				return err
			}
			if !found || record.UpdateSequence != sequence {
				return errors.New("search demand update index is inconsistent")
			}
			page.Records = append(page.Records, record)
		}
		if len(batch.Entries) > 0 {
			sequence, err := sequenceFromKey(batch.Entries[len(batch.Entries)-1].Key)
			if err != nil {
				return err
			}
			page.NextCursor = strconv.FormatUint(sequence, 10)
		} else {
			page.NextCursor = cursor
		}
		return nil
	}); err != nil {
		return Page{}, fmt.Errorf("read search demand updates: %w", err)
	}
	return page, nil
}

func sequenceKey(value uint64) vault.Key {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded
}

func sequenceFromKey(encoded []byte) (uint64, error) {
	if len(encoded) != 8 {
		return 0, errors.New("invalid search demand update index key")
	}
	return binary.BigEndian.Uint64(encoded), nil
}
