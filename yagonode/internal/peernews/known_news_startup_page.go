package peernews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) pruneKnownNewsPage(
	ctx context.Context,
	now time.Time,
	after vault.Key,
	newest *boundedNewestNews,
) (newsPruneProgress, error) {
	var progress newsPruneProgress
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		progress = newsPruneProgress{}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read known news retention page: %w", err)
		}
		page, err := tx.ReadBucketKeyPage(knownBucket, after, newsScrubPage)
		if err != nil {
			return fmt.Errorf("read known news retention page: %w", err)
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
			if err := p.pruneKnownNewsRecord(tx, candidate, now, newest); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return newsPruneProgress{}, fmt.Errorf("prune known news page: %w", err)
	}

	return progress, nil
}

func (p *Pool) pruneKnownNewsRecord(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
	newest *boundedNewestNews,
) error {
	record, valid, err := p.retainedKnownNewsRecord(tx, key, now)
	if err != nil {
		return err
	}
	if !valid {
		return p.deleteInvalidKnownNews(tx, key)
	}
	for _, evicted := range newest.Add(record) {
		if err := p.deleteInvalidKnownNews(tx, evicted.key); err != nil {
			return err
		}
	}

	return nil
}

func (p *Pool) retainedKnownNewsRecord(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
) (retainedNewsRecord, bool, error) {
	present, err := p.storedKnownMarkerPresent(tx, key)
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return retainedNewsRecord{}, false, fmt.Errorf(
				"read retained known news marker: %w", err,
			)
		}
		present = false
	}
	if !present {
		return retainedNewsRecord{}, false, nil
	}
	_, _, categoryErr := p.storedKnownCategoryEvidence(tx, key)
	if categoryErr != nil {
		if !errors.Is(categoryErr, vault.ErrCorruptValue) {
			return retainedNewsRecord{}, false, fmt.Errorf(
				"read retained known news category: %w", categoryErr,
			)
		}
		if _, deleteErr := p.knownCategories.Delete(tx, key); deleteErr != nil {
			return retainedNewsRecord{}, false, fmt.Errorf(
				"discard corrupt known news category: %w", deleteErr,
			)
		}
	}
	created, validCreation := knownNewsCreation(key)
	if !validCreation || !newsCreationAdmitted(created, now, knownMarker) {
		return retainedNewsRecord{}, false, nil
	}

	return retainedNewsRecord{key: key, tie: key, created: created}, true, nil
}

func knownNewsCreation(key vault.Key) (time.Time, bool) {
	created, err := newsIDCreation(string(key))

	return created, err == nil
}
