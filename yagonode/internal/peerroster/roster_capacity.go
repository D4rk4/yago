package peerroster

import (
	"container/heap"
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const peerOverflowDeletionPageSize = 256

func (r *roster) initializeRosterCapacity(ctx context.Context) error {
	retained, total, err := r.rosterEntriesWithinCapacity(ctx)
	if err != nil {
		return err
	}
	if total > len(retained) {
		if _, err := r.deleteRosterEntriesOutside(ctx, retained); err != nil {
			return err
		}
	}
	r.replaceEndpointOwnership(retained)

	return nil
}

func (r *roster) trimOverflow(ctx context.Context) (bool, error) {
	total, err := r.ObservedKnownPeerCount(ctx)
	if err != nil {
		return false, err
	}
	if total <= max(r.reservoirCap, 0) {
		return false, nil
	}
	retained, actual, err := r.rosterEntriesWithinCapacity(ctx)
	if err != nil {
		return false, err
	}
	if actual <= len(retained) {
		return false, nil
	}
	changed, err := r.deleteRosterEntriesOutside(ctx, retained)
	if err != nil {
		return false, err
	}
	if changed {
		r.replaceEndpointOwnership(retained)
	}

	return changed, nil
}

func (r *roster) rosterEntriesWithinCapacity(
	ctx context.Context,
) ([]rosterEntry, int, error) {
	capacity := max(r.reservoirCap, 0)
	_, active := r.activeSnapshot()
	selectionCapacity := max(capacity, len(active))
	retainedInactive := &rankedRosterEntries{precedes: rosterRetentionPrecedes}
	retainedActive := make([]rosterEntry, 0, len(active))
	total := 0
	now := r.now()
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.scanRosterEntries(tx, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if cause := context.Cause(ctx); cause != nil {
				return false, fmt.Errorf("scan peer roster capacity context: %w", cause)
			}
			total++
			if r.isSelf(entry.seed.Hash) ||
				(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) {
				return true, nil
			}
			if _, found := active[entry.seed.Hash]; found {
				retainedActive = append(retainedActive, entry)
				return true, nil
			}
			retainedInactive.retain(entry, selectionCapacity)

			return true, nil
		})
	}); err != nil {
		return nil, 0, fmt.Errorf("scan peer roster capacity: %w", err)
	}
	capacity = max(capacity, len(retainedActive))
	for len(retainedInactive.entries) > capacity-len(retainedActive) {
		heap.Pop(retainedInactive)
	}
	retained := make([]rosterEntry, 0, len(retainedActive)+len(retainedInactive.entries))
	retained = append(retained, retainedActive...)
	retained = append(retained, retainedInactive.entries...)
	sort.Slice(retained, func(left, right int) bool {
		return rosterRetentionPrecedes(retained[left], retained[right])
	})

	return retained, total, nil
}

func rosterRetentionPrecedes(left, right rosterEntry) bool {
	return endpointOwnershipPrecedes(left, right)
}

func (r *roster) deleteRosterEntriesOutside(
	ctx context.Context,
	retained []rosterEntry,
) (bool, error) {
	retainedHashes := rosterEntryHashes(retained)
	changed := false
	var after vault.Key
	for {
		page, pageChanged, err := r.deleteRosterPage(ctx, after, retainedHashes)
		if err != nil {
			return false, err
		}
		changed = changed || pageChanged
		if len(page.Keys) == 0 || !page.More {
			break
		}
		after = append(vault.Key(nil), page.Keys[len(page.Keys)-1]...)
	}
	return changed, nil
}

func rosterEntryHashes(entries []rosterEntry) map[string]struct{} {
	hashes := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		hashes[entry.seed.Hash.String()] = struct{}{}
	}

	return hashes
}

func (r *roster) deleteRosterPage(
	ctx context.Context,
	after vault.Key,
	retainedHashes map[string]struct{},
) (vault.BucketKeyPage, bool, error) {
	var page vault.BucketKeyPage
	changed := false
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		page, err = tx.ReadBucketKeyPage(peersBucket, after, peerOverflowDeletionPageSize)
		if err != nil {
			return fmt.Errorf("read peer roster page: %w", err)
		}
		for _, key := range page.Keys {
			if _, found := retainedHashes[string(key)]; found {
				continue
			}
			deleted, err := r.deleteRosterEntry(tx, key)
			if err != nil {
				return fmt.Errorf("delete peer: %w", err)
			}
			changed = changed || deleted
		}

		return nil
	}); err != nil {
		return vault.BucketKeyPage{}, false, fmt.Errorf("trim peer roster page: %w", err)
	}

	return page, changed, nil
}
