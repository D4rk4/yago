package peernews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) pruneQueuedNewsPage(
	ctx context.Context,
	now time.Time,
	after vault.Key,
	newest *boundedNewestNews,
	catalog queuedNewsEvidenceCatalog,
) (newsPruneProgress, error) {
	var progress newsPruneProgress
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		progress = newsPruneProgress{}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read queued news retention page: %w", err)
		}
		page, err := tx.ReadBucketKeyPage(queueBucket, after, newsScrubPage)
		if err != nil {
			return fmt.Errorf("read queued news retention page: %w", err)
		}
		if len(page.Keys) == 0 {
			return nil
		}
		key := page.Keys[len(page.Keys)-1]
		progress = newsPruneProgress{
			after: append(vault.Key(nil), key...),
			more:  page.More,
		}

		for _, candidate := range page.Keys {
			if err := p.pruneQueuedNewsRecord(tx, candidate, now, newest, catalog); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return newsPruneProgress{}, fmt.Errorf("prune queued news page: %w", err)
	}

	return progress, nil
}

func (p *Pool) pruneQueuedNewsRecord(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
	newest *boundedNewestNews,
	catalog queuedNewsEvidenceCatalog,
) error {
	record, valid, err := p.retainedQueuedNewsRecord(tx, key, now, catalog)
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return err
		}

		return p.deleteInvalidQueuedNews(tx, key)
	}
	if !valid {
		return p.deleteInvalidQueuedNews(tx, key)
	}
	for _, evicted := range newest.Add(record) {
		if err := p.deleteInvalidQueuedNews(tx, evicted.key); err != nil {
			return err
		}
	}

	return nil
}

func (p *Pool) retainedQueuedNewsRecord(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
	catalog queuedNewsEvidenceCatalog,
) (retainedNewsRecord, bool, error) {
	wire, size, queue, err := p.readQueuedNewsWire(tx, key)
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return retainedNewsRecord{}, false, nil
		}

		return retainedNewsRecord{}, false, err
	}
	record, valid := parseQueuedNewsRecord(wire)
	if !valid {
		return retainedNewsRecord{}, false, fmt.Errorf(
			"%w: invalid queued news record", vault.ErrCorruptValue,
		)
	}
	valid, err = p.reconcileQueuedNewsMembership(tx, record, now, catalog)
	if err != nil || !valid {
		return retainedNewsRecord{}, false, err
	}

	return retainedNewsRecord{
		key: key, tie: vault.Key(record.ID() + "\x00" + string(queue)),
		created: record.Created, bytes: size,
	}, true, nil
}

func (p *Pool) readQueuedNewsWire(
	tx *vault.Txn,
	key vault.Key,
) (string, int, Queue, error) {
	size, _, err := p.queue.EncodedSize(tx, key)
	if err != nil {
		return "", size, "", fmt.Errorf("inspect queued news: %w", err)
	}
	queue, validKey := parseQueueKey(key)
	if !validKey {
		return "", size, "", fmt.Errorf(
			"%w: invalid queued news key", vault.ErrCorruptValue,
		)
	}
	if size > maximumNewsRecordBytes {
		return "", size, queue, fmt.Errorf(
			"%w: %w: queued news record size %d",
			vault.ErrCorruptValue,
			ErrBadNewsRecord,
			size,
		)
	}
	wire, present, err := p.queue.Get(tx, key)
	if err != nil {
		return "", size, queue, fmt.Errorf("read queued news: %w", err)
	}
	if !present {
		return "", size, queue, fmt.Errorf(
			"%w: queued news record disappeared during read",
			vault.ErrCorruptValue,
		)
	}

	return wire, size, queue, nil
}

func parseQueuedNewsRecord(wire string) (Record, bool) {
	record, err := parseRecord(wire, time.Time{})

	return record, err == nil
}

func (p *Pool) reconcileQueuedNewsMembership(
	tx *vault.Txn,
	record Record,
	now time.Time,
	catalog queuedNewsEvidenceCatalog,
) (bool, error) {
	key := vault.Key(record.ID())
	known, err := p.storedKnownMarkerPresent(tx, key)
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return false, nil
		}

		return false, fmt.Errorf("read queued news membership: %w", err)
	}
	if !known {
		return false, nil
	}
	evidence, selected := catalog[record.ID()]
	if !newsCreationAdmitted(record.Created, now, record.Category) {
		if selected {
			return false, nil
		}
		if err := p.forgetKnownNews(tx, key); err != nil {
			return false, fmt.Errorf("evict expired queued news membership: %w", err)
		}

		return false, nil
	}
	if !selected || !queuedNewsMatchesEvidence(record, evidence) {
		return false, nil
	}
	encoded, exact, err := p.storedKnownCategoryEvidence(tx, key)
	if err == nil && exact && encoded == knownCategoryEvidence(record) {
		return newsCreationAdmitted(record.Created, now, record.Category), nil
	}
	if err != nil && !errors.Is(err, vault.ErrCorruptValue) {
		return false, fmt.Errorf("read queued news category: %w", err)
	}
	if err := p.replaceKnownNewsCategoryForRecord(tx, key, record); err != nil {
		return false, fmt.Errorf("migrate queued news membership: %w", err)
	}
	return true, nil
}
