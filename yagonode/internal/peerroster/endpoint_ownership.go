package peerroster

import (
	"context"
	"fmt"
	"net"
	"sort"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type endpointOwnership struct {
	peer     yagomodel.Hash
	verified bool
}

type endpointAdmission struct {
	displaced []yagomodel.Hash
	accepted  bool
}

func (r *roster) rebuildEndpointOwnership(ctx context.Context) error {
	limit := min(max(r.reservoirCap, 0), peerDiscoveryMaximumSeeds)
	if limit == 0 {
		r.endpointMu.Lock()
		r.endpointOwners = make(map[string]endpointOwnership)
		r.endpointMu.Unlock()

		return nil
	}
	now := r.now()
	retained := &rankedRosterEntries{precedes: endpointOwnershipPrecedes}
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.scanRosterEntries(tx, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if cause := context.Cause(ctx); cause != nil {
				return false, fmt.Errorf("scan peer endpoints context: %w", cause)
			}
			if r.isSelf(entry.seed.Hash) {
				return true, nil
			}
			if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
				return true, nil
			}
			retained.retain(entry, limit)

			return true, nil
		})
	}); err != nil {
		return fmt.Errorf("scan peer endpoints: %w", err)
	}
	entries := retained.entries
	r.replaceEndpointOwnership(entries)

	return nil
}

func (r *roster) replaceEndpointOwnership(entries []rosterEntry) {
	sort.Slice(entries, func(left, right int) bool {
		return endpointOwnershipPrecedes(entries[left], entries[right])
	})
	owners := make(map[string]endpointOwnership, len(entries))
	for _, entry := range entries {
		admission := endpointAdmissionAgainst(owners, entry)
		if admission.accepted {
			applyEndpointAdmissionTo(owners, entry, admission)
		}
	}
	r.endpointMu.Lock()
	r.endpointOwners = owners
	r.endpointMu.Unlock()
}

func endpointOwnershipPrecedes(left, right rosterEntry) bool {
	if left.verified != right.verified {
		return left.verified
	}
	comparison := left.lastSeen.Compare(right.lastSeen)
	if comparison != 0 {
		return comparison > 0
	}

	return left.seed.Hash.String() < right.seed.Hash.String()
}

func (r *roster) endpointAdmission(entry rosterEntry) endpointAdmission {
	return endpointAdmissionAgainst(r.endpointOwnershipSnapshot(), entry)
}

func endpointAdmissionAgainst(
	owners map[string]endpointOwnership,
	entry rosterEntry,
) endpointAdmission {
	displaced := make(map[yagomodel.Hash]struct{})
	for _, endpoint := range advertisedPeerEndpoints(entry.seed) {
		owner, found := owners[endpoint]
		if !found || owner.peer == entry.seed.Hash {
			continue
		}
		if !entry.verified || owner.verified {
			return endpointAdmission{}
		}
		displaced[owner.peer] = struct{}{}
	}
	peers := make([]yagomodel.Hash, 0, len(displaced))
	for peer := range displaced {
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(left, right int) bool {
		return peers[left].String() < peers[right].String()
	})

	return endpointAdmission{displaced: peers, accepted: true}
}

func applyEndpointAdmissionTo(
	owners map[string]endpointOwnership,
	entry rosterEntry,
	admission endpointAdmission,
) {
	for _, peer := range admission.displaced {
		removeEndpointOwnershipFrom(owners, peer)
	}
	removeEndpointOwnershipFrom(owners, entry.seed.Hash)
	for _, endpoint := range advertisedPeerEndpoints(entry.seed) {
		owners[endpoint] = ownershipFor(entry)
	}
}

func (r *roster) applyEndpointAdmission(entry rosterEntry, admission endpointAdmission) {
	r.endpointMu.Lock()
	defer r.endpointMu.Unlock()
	applyEndpointAdmissionTo(r.endpointOwners, entry, admission)
}

func (r *roster) endpointOwnershipSnapshot() map[string]endpointOwnership {
	r.endpointMu.RLock()
	defer r.endpointMu.RUnlock()

	owners := make(map[string]endpointOwnership, len(r.endpointOwners))
	for endpoint, owner := range r.endpointOwners {
		owners[endpoint] = owner
	}

	return owners
}

func removeEndpointOwnershipFrom(
	owners map[string]endpointOwnership,
	peer yagomodel.Hash,
) {
	for endpoint, owner := range owners {
		if owner.peer == peer {
			delete(owners, endpoint)
		}
	}
}

func advertisedPeerEndpoints(seed yagomodel.Seed) []string {
	port, known := seed.Port.Get()
	if !known {
		return nil
	}
	hosts := seed.AdvertisedHosts()
	endpoints := make([]string, 0, len(hosts))
	for _, host := range hosts {
		endpoints = append(endpoints, net.JoinHostPort(
			host.String(),
			port.String(),
		))
	}

	return endpoints
}

func ownershipFor(entry rosterEntry) endpointOwnership {
	return endpointOwnership{
		peer:     entry.seed.Hash,
		verified: entry.verified,
	}
}

func entryOwnsAdvertisedEndpoints(
	owners map[string]endpointOwnership,
	entry rosterEntry,
) bool {
	endpoints := advertisedPeerEndpoints(entry.seed)
	if len(endpoints) == 0 {
		return false
	}
	for _, endpoint := range endpoints {
		owner, found := owners[endpoint]
		if !found || owner.peer != entry.seed.Hash {
			return false
		}
	}

	return true
}
