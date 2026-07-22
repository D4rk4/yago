package peernews

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maximumNewsCleanupValueBytes = ((maximumNewsRecordBytes+2)/3)*4 + 512

var (
	knownCleanupCursorKey    = vault.Key("known")
	categoryCleanupCursorKey = vault.Key("category")
	queuedCleanupCursorKey   = vault.Key("queued")
	errStaleNewsCleanup      = errors.New("stale news cleanup cursor")
)

type newsCleanupCodec struct{}

func (newsCleanupCodec) Encode(value string) ([]byte, error) {
	if err := validateNewsCleanupCursor(value); err != nil {
		return nil, err
	}

	return []byte(value), nil
}

func (newsCleanupCodec) Decode(raw []byte) (string, error) {
	value := string(raw)
	if err := validateNewsCleanupCursor(value); err != nil {
		return "", err
	}

	return value, nil
}

func validateNewsCleanupCursor(value string) error {
	if value == "" || len(value) > maximumNewsCleanupValueBytes {
		return fmt.Errorf("invalid news cleanup cursor")
	}

	return nil
}

func (p *Pool) cleanupCursor(ctx context.Context, name vault.Key) (vault.Key, error) {
	var after vault.Key
	corrupt := false
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		value, found, err := p.storedNewsCleanup(tx, name)
		if err != nil {
			return err
		}
		if found {
			after = vault.Key(value)
		}

		return nil
	})
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return nil, fmt.Errorf("read news cleanup cursor: %w", err)
		}
		corrupt = true
	}
	if corrupt {
		if err := p.clearCleanupCursor(ctx, name); err != nil {
			return nil, fmt.Errorf("discard news cleanup cursor: %w", err)
		}
	}

	return after, nil
}

func (p *Pool) storedNewsCleanup(
	tx *vault.Txn,
	key vault.Key,
) (string, bool, error) {
	size, found, err := p.cleanup.EncodedSize(tx, key)
	if err != nil {
		return "", found, fmt.Errorf("inspect news cleanup value: %w", err)
	}
	if !found {
		return "", false, nil
	}
	if size > maximumNewsCleanupValueBytes {
		return "", true, fmt.Errorf(
			"%w: invalid news cleanup value size %d", vault.ErrCorruptValue, size,
		)
	}

	value, present, err := p.cleanup.Get(tx, key)
	if err != nil {
		return "", true, fmt.Errorf("read news cleanup value: %w", err)
	}
	if !present {
		return "", true, fmt.Errorf(
			"%w: news cleanup value disappeared during read", vault.ErrCorruptValue,
		)
	}

	return value, true, nil
}

func (p *Pool) storeCleanupCursor(ctx context.Context, name, after vault.Key) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := p.cleanup.Put(tx, name, string(after)); err != nil {
			return fmt.Errorf("store news cleanup cursor: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update news cleanup cursor: %w", err)
	}

	return nil
}

func (p *Pool) clearCleanupCursor(ctx context.Context, name vault.Key) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := p.cleanup.Delete(tx, name); err != nil {
			return fmt.Errorf("clear news cleanup cursor: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update cleared news cleanup cursor: %w", err)
	}

	return nil
}

func (p *Pool) clearCleanupCursors(ctx context.Context) error {
	for _, name := range []vault.Key{
		knownCleanupCursorKey,
		categoryCleanupCursorKey,
		queuedCleanupCursorKey,
	} {
		if err := p.clearCleanupCursor(ctx, name); err != nil {
			return err
		}
	}

	return nil
}

func isStaleNewsCleanupCursor(err error) bool {
	return errors.Is(err, errStaleNewsCleanup)
}

func (p *Pool) restoreKnownNewsPrefix(
	ctx context.Context,
	now time.Time,
	through vault.Key,
	newest *boundedNewestNews,
) error {
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return forNewsKeysThrough(tx, knownBucket, through, func(key vault.Key) error {
			record, valid, err := p.readKnownNewsPrefixRecord(tx, key, now)
			if err != nil {
				return err
			}
			if !valid || len(newest.Add(record)) != 0 {
				return errStaleNewsCleanup
			}

			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("restore known news prefix: %w", err)
	}

	return nil
}

func (p *Pool) readKnownNewsPrefixRecord(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
) (retainedNewsRecord, bool, error) {
	_, err := p.storedKnownMarkerPresent(tx, key)
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return retainedNewsRecord{}, false, nil
		}

		return retainedNewsRecord{}, false, fmt.Errorf("read known news prefix: %w", err)
	}
	created, valid := knownNewsCreation(key)
	if !valid || !newsCreationAdmitted(created, now, knownMarker) {
		return retainedNewsRecord{}, false, nil
	}

	return retainedNewsRecord{key: key, tie: key, created: created}, true, nil
}

func (p *Pool) restoreQueuedNewsPrefix(
	ctx context.Context,
	now time.Time,
	through vault.Key,
	newest *boundedNewestNews,
	catalog queuedNewsEvidenceCatalog,
) error {
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return forNewsKeysThrough(tx, queueBucket, through, func(key vault.Key) error {
			record, retained, err := p.readQueuedNewsPrefixRecord(tx, key, now, catalog)
			if err != nil {
				return err
			}
			if !retained || len(newest.Add(record)) != 0 {
				return errStaleNewsCleanup
			}

			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("restore queued news prefix: %w", err)
	}

	return nil
}

func (p *Pool) readQueuedNewsPrefixRecord(
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
	record, decoded := decodeQueuedNewsRecord(wire)
	if !decoded {
		return retainedNewsRecord{}, false, nil
	}
	known, err := p.storedKnownMarkerPresent(tx, vault.Key(record.ID()))
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return retainedNewsRecord{}, false, nil
		}

		return retainedNewsRecord{}, false, fmt.Errorf(
			"read queued news prefix membership: %w",
			err,
		)
	}
	evidence, selected := catalog[record.ID()]
	if !known || !selected || !queuedNewsMatchesEvidence(record, evidence) ||
		!newsCreationAdmitted(record.Created, now, record.Category) {
		return retainedNewsRecord{}, false, nil
	}

	return retainedNewsRecord{
		key: key, tie: vault.Key(record.ID() + "\x00" + string(queue)),
		created: record.Created, bytes: size,
	}, true, nil
}

func (p *Pool) validateKnownCategoryPrefix(ctx context.Context, through vault.Key) error {
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return forNewsKeysThrough(tx, knownCategoryBucket, through, func(key vault.Key) error {
			_, categoryFound, categoryErr := p.storedKnownCategoryEvidence(tx, key)
			if categoryErr != nil && !errors.Is(categoryErr, vault.ErrCorruptValue) {
				return categoryErr
			}
			markerFound, markerErr := p.storedKnownMarkerPresent(tx, key)
			if markerErr != nil && !errors.Is(markerErr, vault.ErrCorruptValue) {
				return markerErr
			}
			if categoryErr != nil || !categoryFound || markerErr != nil || !markerFound {
				return errStaleNewsCleanup
			}

			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("validate known news category prefix: %w", err)
	}

	return nil
}

func forNewsKeysThrough(
	tx *vault.Txn,
	bucket vault.Name,
	through vault.Key,
	visit func(vault.Key) error,
) error {
	var after vault.Key
	for {
		page, err := tx.ReadBucketKeyPage(bucket, after, newsScrubPage)
		if err != nil {
			return fmt.Errorf("read news cleanup prefix: %w", err)
		}
		for _, key := range page.Keys {
			if bytes.Compare(key, through) > 0 {
				return nil
			}
			if err := visit(key); err != nil {
				return err
			}
		}
		if len(page.Keys) == 0 || !page.More {
			return nil
		}
		after = page.Keys[len(page.Keys)-1]
	}
}
