package peerroster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDiscoveryPreparationCancelsTheWholeBatch(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	ctx, cancel := context.WithCancel(t.Context())
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.now = func() time.Time {
		cancel()

		return time.Unix(100, 0)
	}

	if prepared := r.prepareDiscoveries(ctx, []yagomodel.Seed{first, second}); prepared != nil {
		t.Fatalf("partially prepared canceled batch = %#v", prepared)
	}
}

func TestDiscoveryPersistenceStopsWhenItsContextIsCanceled(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	entry, _ := discoveredRosterEntry(peer, r.now())
	persistence := &discoveryPersistence{
		owners:      map[string]endpointOwnership{},
		displaced:   map[yagomodel.Hash]struct{}{},
		storedPeers: map[yagomodel.Hash]struct{}{},
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.persistPreparedDiscovery(
			canceled,
			tx,
			preparedDiscovery{entry: entry, key: r.key(peer.Hash)},
			persistence,
		)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery persistence error = %v", err)
	}
}

func TestVerifiedDiscoveryRefreshDisplacesAnUnverifiedEndpointOwner(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	owner := timedInternalSeed(t, "owner", "203.0.113.1", now.Add(-time.Hour))
	r.Discover(t.Context(), owner)
	refreshed := timedInternalSeed(t, "refreshed", "203.0.113.2", now.Add(-time.Hour))
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(refreshed.Hash), rosterEntry{
			seed:       refreshed,
			lastSeen:   now.Add(-2 * time.Hour),
			expiresAt:  now.Add(-time.Second),
			verified:   true,
			retryAfter: now,
		})
	}); err != nil {
		t.Fatalf("store expired verified peer: %v", err)
	}
	refreshed.IP = owner.IP
	refreshed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now))

	r.Discover(t.Context(), refreshed)

	candidates := r.FreshestPeers(t.Context(), 8)
	if len(candidates) != 1 || candidates[0].Hash != refreshed.Hash {
		t.Fatalf("displaced discovery candidates = %#v", candidates)
	}
}

func TestActiveMembershipRemovesEveryDisplacedPeer(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	entry := verifiedRosterEntry(second, r.now())
	r.active[first.Hash] = verifiedRosterEntry(first, r.now())

	r.replaceActiveMembership(entry, []yagomodel.Hash{first.Hash})

	if _, found := r.active[first.Hash]; found {
		t.Fatal("displaced peer remained active")
	}
}
