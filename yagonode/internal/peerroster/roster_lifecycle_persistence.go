package peerroster

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (r *roster) getRosterEntry(
	tx *vault.Txn,
	key vault.Key,
) (rosterEntry, bool, error) {
	entry, found, err := r.peers.Get(tx, key)
	if err != nil {
		return rosterEntry{}, false, fmt.Errorf("read roster peer: %w", err)
	}
	if !found {
		return entry, false, nil
	}
	entry = r.attachRosterLifecycle(tx, key, entry)

	return entry, true, nil
}

func (r *roster) attachRosterLifecycle(
	tx *vault.Txn,
	key vault.Key,
	entry rosterEntry,
) rosterEntry {
	lifecycle, found, err := r.lifecycles.Get(tx, key)
	if err != nil {
		return conservativeRosterEntry(entry)
	}
	if !found {
		return entry
	}
	if !lifecycle.appliesTo(entry) {
		return conservativeRosterEntry(entry)
	}
	entry.retryAfter = lifecycle.retryAfter
	entry.expiresAt = lifecycle.expiresAt
	entry.verified = lifecycle.verified

	return entry
}

func (r *roster) scanRosterEntries(
	tx *vault.Txn,
	fn func(vault.Key, rosterEntry) (bool, error),
) error {
	err := r.peers.Scan(tx, nil, func(key vault.Key, entry rosterEntry) (bool, error) {
		entry = r.attachRosterLifecycle(tx, key, entry)

		return fn(key, entry)
	})
	if err != nil {
		return fmt.Errorf("scan roster peers: %w", err)
	}

	return nil
}

func (r *roster) putRosterEntry(
	tx *vault.Txn,
	key vault.Key,
	entry rosterEntry,
) error {
	lifecycle, err := rosterLifecycleFor(entry)
	if err != nil {
		return fmt.Errorf("prepare roster lifecycle: %w", err)
	}
	if err := r.peers.Put(tx, key, entry); err != nil {
		return fmt.Errorf("store roster peer: %w", err)
	}
	if err := r.lifecycles.Put(tx, key, lifecycle); err != nil {
		return fmt.Errorf("store roster lifecycle: %w", err)
	}

	return nil
}

func (r *roster) deleteRosterEntry(tx *vault.Txn, key vault.Key) (bool, error) {
	removed, err := r.peers.Delete(tx, key)
	if err != nil {
		return false, fmt.Errorf("delete roster peer: %w", err)
	}
	if _, err := r.lifecycles.Delete(tx, key); err != nil {
		return false, fmt.Errorf("delete roster lifecycle: %w", err)
	}

	return removed, nil
}

func conservativeRosterEntry(entry rosterEntry) rosterEntry {
	return rosterEntry{
		seed:      entry.seed,
		lastSeen:  entry.lastSeen,
		expiresAt: entry.lastSeen.Add(peerPassiveRetention),
	}
}
