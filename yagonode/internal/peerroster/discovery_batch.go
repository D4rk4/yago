package peerroster

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type preparedDiscovery struct {
	entry rosterEntry
	key   vault.Key
}

type discoveryPersistence struct {
	owners      map[string]endpointOwnership
	displaced   map[yagomodel.Hash]struct{}
	storedPeers map[yagomodel.Hash]struct{}
	changed     bool
}

func (r *roster) prepareDiscoveries(
	ctx context.Context,
	seeds []yagomodel.Seed,
) []preparedDiscovery {
	prepared := make([]preparedDiscovery, 0, len(seeds))
	for _, seed := range seeds {
		if context.Cause(ctx) != nil {
			return nil
		}
		if r.isSelf(seed.Hash) {
			continue
		}
		if _, reachable := seed.NetworkAddress(); !reachable ||
			!routingClassificationEligible(seed) {
			continue
		}
		entry, fresh := discoveredRosterEntry(seed, r.now())
		if !fresh {
			continue
		}
		if _, err := (rosterEntryCodec{}).Encode(entry); err != nil {
			continue
		}
		prepared = append(prepared, preparedDiscovery{entry: entry, key: r.key(seed.Hash)})
	}
	sort.Slice(prepared, func(left, right int) bool {
		comparison := prepared[left].entry.lastSeen.Compare(prepared[right].entry.lastSeen)
		if comparison != 0 {
			return comparison > 0
		}

		return prepared[left].entry.seed.Hash.String() <
			prepared[right].entry.seed.Hash.String()
	})

	return prepared
}

func (r *roster) persistDiscoveryBatch(
	ctx context.Context,
	prepared []preparedDiscovery,
) (bool, error) {
	if len(prepared) == 0 {
		return false, nil
	}
	persistence := &discoveryPersistence{
		owners:      r.endpointOwnershipSnapshot(),
		displaced:   make(map[yagomodel.Hash]struct{}),
		storedPeers: make(map[yagomodel.Hash]struct{}),
	}
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, discovery := range prepared {
			if err := r.persistPreparedDiscovery(ctx, tx, discovery, persistence); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return false, fmt.Errorf("discover peers: %w", err)
	}
	if !persistence.changed {
		return false, nil
	}
	r.applyDiscoveryPersistence(persistence)

	return true, nil
}

func (r *roster) persistPreparedDiscovery(
	ctx context.Context,
	tx *vault.Txn,
	discovery preparedDiscovery,
	persistence *discoveryPersistence,
) error {
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("discover peer context: %w", cause)
	}
	stored, known, err := r.getRosterEntry(tx, discovery.key)
	if err != nil {
		return fmt.Errorf("read peer: %w", err)
	}
	if known {
		now := r.now()
		storedExpired := !stored.expiresAt.IsZero() && !now.Before(stored.expiresAt)
		if !discovery.entry.lastSeen.After(stored.lastSeen) ||
			(stored.verified && !storedExpired) {
			return nil
		}
		discovery.entry.verified = stored.verified
	}
	admission := endpointAdmissionAgainst(persistence.owners, discovery.entry)
	if !admission.accepted {
		return nil
	}
	if err := r.putRosterEntry(tx, discovery.key, discovery.entry); err != nil {
		return fmt.Errorf("store peer: %w", err)
	}
	for _, peer := range admission.displaced {
		persistence.displaced[peer] = struct{}{}
	}
	applyEndpointAdmissionTo(persistence.owners, discovery.entry, admission)
	persistence.storedPeers[discovery.entry.seed.Hash] = struct{}{}
	persistence.changed = true

	return nil
}

func (r *roster) applyDiscoveryPersistence(persistence *discoveryPersistence) {
	r.endpointMu.Lock()
	r.endpointOwners = persistence.owners
	r.endpointMu.Unlock()
	r.mu.Lock()
	for peer := range persistence.storedPeers {
		delete(r.active, peer)
	}
	for peer := range persistence.displaced {
		delete(r.active, peer)
	}
	r.mu.Unlock()
}
