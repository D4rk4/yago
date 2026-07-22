package peerroster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	rosterLifecycleCleanupLimit    = peerDiscoveryMaximumSeeds
	rosterLifecycleCleanupPageSize = 256
)

func (r *roster) cleanupRosterLifecycleOrphans(ctx context.Context) error {
	remaining := rosterLifecycleCleanupLimit
	after, err := r.loadRosterLifecycleCleanupCursor(ctx)
	if err != nil {
		return err
	}
	for remaining > 0 {
		pageLimit := min(rosterLifecycleCleanupPageSize, remaining)
		page, err := r.cleanupRosterLifecyclePage(ctx, after, pageLimit)
		if err != nil {
			return err
		}
		remaining -= len(page.Keys)
		if len(page.Keys) == 0 || !page.More {
			return nil
		}
		after = append(vault.Key(nil), page.Keys[len(page.Keys)-1]...)
	}

	return nil
}

func (r *roster) loadRosterLifecycleCleanupCursor(ctx context.Context) (vault.Key, error) {
	var cursor vault.Key
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		stored, found, err := r.lifecycleCleanupCursor.Get(
			tx,
			rosterLifecycleCleanupCursorKey,
		)
		if err != nil {
			if _, deleteErr := r.lifecycleCleanupCursor.Delete(
				tx,
				rosterLifecycleCleanupCursorKey,
			); deleteErr != nil {
				return fmt.Errorf("discard roster lifecycle cleanup cursor: %w", deleteErr)
			}

			return nil
		}
		if found {
			cursor = append(vault.Key(nil), stored...)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("load roster lifecycle cleanup cursor: %w", err)
	}

	return cursor, nil
}

func (r *roster) cleanupRosterLifecyclePage(
	ctx context.Context,
	after vault.Key,
	limit int,
) (vault.BucketKeyPage, error) {
	var page vault.BucketKeyPage
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		page, err = tx.ReadBucketKeyPage(peerLifecyclesBucket, after, limit)
		if err != nil {
			return fmt.Errorf("read roster lifecycle page: %w", err)
		}
		if err := r.cleanupRosterLifecycleRows(tx, page.Keys); err != nil {
			return err
		}

		return r.persistRosterLifecycleCleanupCursor(tx, page)
	}); err != nil {
		return vault.BucketKeyPage{}, fmt.Errorf("clean roster lifecycle page: %w", err)
	}

	return page, nil
}

func (r *roster) cleanupRosterLifecycleRows(tx *vault.Txn, keys []vault.Key) error {
	for _, key := range keys {
		if err := r.cleanupRosterLifecycleRow(tx, key); err != nil {
			return err
		}
	}

	return nil
}

func (r *roster) cleanupRosterLifecycleRow(tx *vault.Txn, key vault.Key) error {
	lifecycle, _, err := r.lifecycles.Get(tx, key)
	if err != nil {
		if _, deleteErr := r.lifecycles.Delete(tx, key); deleteErr != nil {
			return fmt.Errorf("discard malformed roster lifecycle: %w", deleteErr)
		}

		return nil
	}
	entry, peerFound, err := r.peers.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read lifecycle peer: %w", err)
	}
	if peerFound && lifecycle.appliesTo(entry) {
		return nil
	}
	if _, err := r.lifecycles.Delete(tx, key); err != nil {
		return fmt.Errorf("delete orphan roster lifecycle: %w", err)
	}

	return nil
}

func (r *roster) persistRosterLifecycleCleanupCursor(
	tx *vault.Txn,
	page vault.BucketKeyPage,
) error {
	if page.More {
		if err := r.lifecycleCleanupCursor.Put(
			tx,
			rosterLifecycleCleanupCursorKey,
			page.Keys[len(page.Keys)-1],
		); err != nil {
			return fmt.Errorf("store roster lifecycle cleanup cursor: %w", err)
		}

		return nil
	}
	if _, err := r.lifecycleCleanupCursor.Delete(
		tx,
		rosterLifecycleCleanupCursorKey,
	); err != nil {
		return fmt.Errorf("clear roster lifecycle cleanup cursor: %w", err)
	}

	return nil
}
