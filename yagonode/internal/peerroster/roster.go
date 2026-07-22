package peerroster

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const peersBucket vault.Name = "peerroster"

const (
	peerRemoteIndexRejectedMessage        = "peer remote index rejected"
	peerRemoteIndexRejectionFailedMessage = "peer remote index rejection failed"
	peerDiscoveryMaximumSeeds             = 4096
	peerPassiveRetryCooldown              = 10 * time.Minute
	peerPassiveRetention                  = 24 * time.Hour
)

type roster struct {
	vault                  *vault.Vault
	peers                  *vault.Collection[rosterEntry]
	lifecycles             *vault.Keyspace[rosterLifecycle]
	lifecycleCleanupCursor *vault.Keyspace[vault.Key]
	self                   yagomodel.Hash
	now                    func() time.Time
	reservoirCap           int
	activeCap              int

	membershipPermit chan struct{}
	mu               sync.Mutex
	active           map[yagomodel.Hash]rosterEntry
	endpointMu       sync.RWMutex
	endpointOwners   map[string]endpointOwnership

	candidateMu        sync.Mutex
	candidateRevision  uint64
	candidateReady     bool
	candidateEntries   []rosterEntry
	candidateBytes     int
	candidateBuilding  chan struct{}
	candidateByteLimit int
}

func (r *roster) key(hash yagomodel.Hash) vault.Key {
	return vault.Key(hash.String())
}

func (r *roster) Discover(ctx context.Context, seeds ...yagomodel.Seed) {
	if context.Cause(ctx) != nil {
		return
	}
	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	maximumDiscoveries := min(peerDiscoveryMaximumSeeds, len(seeds))
	prepared := r.prepareDiscoveries(ctx, seeds[:maximumDiscoveries])
	changed, err := r.persistDiscoveryBatch(
		ctx,
		prepared,
	)
	if err != nil {
		if context.Cause(ctx) == nil {
			slog.WarnContext(ctx, "peer discovery batch discarded", slog.Any("error", err))
		}

		return
	}
	changed = r.evictOverflow(ctx) || changed
	if changed {
		r.invalidateCandidateSnapshot()
	}
}

func (r *roster) discoverOne(ctx context.Context, seed yagomodel.Seed) (bool, error) {
	return r.persistDiscoveryBatch(ctx, r.prepareDiscoveries(ctx, []yagomodel.Seed{seed}))
}

func (r *roster) ConfirmReachable(ctx context.Context, peer yagomodel.Hash) {
	if r.isSelf(peer) {
		return
	}
	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	confirmed, admission, found := r.touch(ctx, peer)
	if !found {
		return
	}
	r.applyEndpointAdmission(confirmed, admission)
	r.replaceActiveMembership(confirmed, admission.displaced)
	r.invalidateCandidateSnapshot()
}

func (r *roster) touch(
	ctx context.Context,
	peer yagomodel.Hash,
) (rosterEntry, endpointAdmission, bool) {
	if r.isSelf(peer) {
		return rosterEntry{}, endpointAdmission{}, false
	}
	var confirmed rosterEntry
	var admission endpointAdmission
	found := false
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.getRosterEntry(tx, r.key(peer))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if !known {
			return nil
		}

		now := r.now()
		entry.lastSeen = now
		entry.retryAfter = time.Time{}
		entry.expiresAt = now.Add(peerPassiveRetention)
		entry.verified = true
		admission = r.endpointAdmission(entry)
		if !admission.accepted {
			return nil
		}
		if err := r.putRosterEntry(tx, r.key(peer), entry); err != nil {
			return fmt.Errorf("store peer: %w", err)
		}
		confirmed, found = entry, true

		return nil
	}); err != nil {
		slog.WarnContext(
			ctx,
			"peer reachability not recorded",
			slog.String("peer", peer.String()),
			slog.Any("error", err),
		)

		return rosterEntry{}, endpointAdmission{}, false
	}

	return confirmed, admission, found
}

func (r *roster) RejectRemoteIndex(ctx context.Context, failed yagomodel.Seed) {
	if r.isSelf(failed.Hash) {
		return
	}
	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	var updated yagomodel.Seed
	changed := false
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.getRosterEntry(tx, r.key(failed.Hash))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if !known || !entry.seed.SharesAddress(failed) {
			return nil
		}
		entry.seed = withoutRemoteIndex(entry.seed)
		if err := r.putRosterEntry(tx, r.key(failed.Hash), entry); err != nil {
			return fmt.Errorf("store peer: %w", err)
		}
		updated = entry.seed
		changed = true

		return nil
	}); err != nil {
		slog.WarnContext(
			ctx,
			peerRemoteIndexRejectionFailedMessage,
			slog.String("peer", failed.Hash.String()),
			slog.Any("error", err),
		)

		return
	}
	if !changed {
		return
	}

	r.mu.Lock()
	delete(r.active, r.self)
	if reachable, active := r.active[failed.Hash]; active {
		reachable.seed = updated
		r.active[failed.Hash] = reachable
	}
	r.mu.Unlock()
	r.invalidateCandidateSnapshot()

	slog.WarnContext(ctx, peerRemoteIndexRejectedMessage, slog.String("peer", failed.Hash.String()))
}

func (r *roster) ReachablePeers(_ context.Context) []yagomodel.Seed {
	owners := r.endpointOwnershipSnapshot()
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	peers := make([]yagomodel.Seed, 0, len(r.active))
	for peer, entry := range r.active {
		if r.isSelf(peer) ||
			(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) ||
			!entryOwnsAdvertisedEndpoints(owners, entry) {
			continue
		}
		peers = append(peers, detachCandidateSeed(entry.seed))
	}
	sort.Slice(peers, func(left, right int) bool {
		leftEntry := r.active[peers[left].Hash]
		rightEntry := r.active[peers[right].Hash]
		comparison := leftEntry.lastSeen.Compare(rightEntry.lastSeen)
		if comparison != 0 {
			return comparison > 0
		}

		return peers[left].Hash.String() < peers[right].Hash.String()
	})

	return peers
}

func withoutRemoteIndex(seed yagomodel.Seed) yagomodel.Seed {
	flags, ok := seed.Flags.Get()
	if !ok {
		flags = yagomodel.ZeroFlags()
	}
	seed.Flags = yagomodel.Some(flags.Set(yagomodel.FlagAcceptRemoteIndex, false))

	return seed
}

// PeerByHash returns the stored seed for a peer hash, or false when the roster has
// never discovered it. It reads through the persisted peers collection, so a peer
// outside the freshest working set is still resolvable for a detail lookup.
func (r *roster) PeerByHash(ctx context.Context, peer yagomodel.Hash) (yagomodel.Seed, bool) {
	if r.isSelf(peer) {
		return yagomodel.Seed{}, false
	}
	var (
		seed  yagomodel.Seed
		found bool
	)
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.getRosterEntry(tx, r.key(peer))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if known {
			seed, found = entry.seed, true
		}

		return nil
	}); err != nil {
		slog.WarnContext(ctx, "peer lookup failed",
			slog.String("peer", peer.String()), slog.Any("error", err))

		return yagomodel.Seed{}, false
	}

	return seed, found
}

func (r *roster) KnownPeerCount(ctx context.Context) int {
	return r.peerCount(ctx)
}

func (r *roster) ReachablePeerCount(_ context.Context) int {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	count := 0
	for peer, entry := range r.active {
		if r.isSelf(peer) ||
			(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) {
			continue
		}
		count++
	}

	return count
}

// Future: a recency index would replace this scan with a bounded range read.
func (r *roster) FreshestPeers(ctx context.Context, limit int) []yagomodel.Seed {
	return r.freshestCandidateSnapshot(ctx, limit)
}

func (r *roster) activeSnapshot() ([]rosterEntry, map[yagomodel.Hash]struct{}) {
	return r.activeSnapshotAgainst(r.endpointOwnershipSnapshot())
}

func (r *roster) activeSnapshotAgainst(
	owners map[string]endpointOwnership,
) ([]rosterEntry, map[yagomodel.Hash]struct{}) {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := make([]rosterEntry, 0, len(r.active))
	keys := make(map[yagomodel.Hash]struct{}, len(r.active))
	for hash, entry := range r.active {
		if r.isSelf(hash) ||
			(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) ||
			!entryOwnsAdvertisedEndpoints(owners, entry) {
			continue
		}
		entry.seed = detachCandidateSeed(entry.seed)
		entries = append(entries, entry)
		keys[hash] = struct{}{}
	}
	sort.Slice(entries, func(left, right int) bool {
		comparison := entries[left].lastSeen.Compare(entries[right].lastSeen)
		if comparison != 0 {
			return comparison > 0
		}

		return entries[left].seed.Hash.String() < entries[right].seed.Hash.String()
	})

	return entries, keys
}

func (r *roster) evictOverflow(ctx context.Context) bool {
	changed, err := r.trimOverflow(ctx)
	if err != nil {
		slog.WarnContext(ctx, "peer eviction failed", slog.Any("error", err))

		return false
	}
	return changed
}

func (r *roster) selectInactive(
	ctx context.Context,
	active map[yagomodel.Hash]struct{},
	limit int,
	precedes func(a, b rosterEntry) bool,
) []rosterEntry {
	if limit <= 0 {
		return nil
	}

	kept := &rankedRosterEntries{precedes: precedes}
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.scanRosterEntries(tx, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("select inactive peer context: %w", err)
			}
			if r.isSelf(entry.seed.Hash) {
				return true, nil
			}
			if _, ok := active[entry.seed.Hash]; ok {
				return true, nil
			}
			kept.retain(entry, limit)

			return true, nil
		})
	}); err != nil {
		slog.WarnContext(ctx, "peer roster scan failed", slog.Any("error", err))

		return nil
	}

	sort.SliceStable(kept.entries, func(left, right int) bool {
		return precedes(kept.entries[left], kept.entries[right])
	})

	return kept.entries
}

func (r *roster) peerCount(ctx context.Context) int {
	total, err := r.ObservedKnownPeerCount(ctx)
	if err != nil {
		slog.WarnContext(ctx, "peer roster count failed", slog.Any("error", err))

		return 0
	}

	return total
}
