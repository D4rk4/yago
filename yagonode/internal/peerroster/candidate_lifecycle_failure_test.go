package peerroster

import (
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestExpiredPassivePruningRemovesEverySelectedPeer(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	for _, peer := range []yagomodel.Seed{first, second} {
		peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
		r.ObserveResponder(t.Context(), peer)
		r.ConfirmUnreachable(t.Context(), peer.Hash)
	}
	now = now.Add(peerPassiveRetention + time.Second)

	r.PruneExpired(t.Context())

	if r.KnownPeerCount(t.Context()) != 0 {
		t.Fatal("expired passive batch remained persisted")
	}
}

func TestExpiredPassiveEvictionPreservesActiveAndHandlesStorageFailures(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		r, _ := openScriptedRoster(t, 8, 4)
		peer := internalSeed(t, "active", "203.0.113.1")
		peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
		r.ObserveResponder(t.Context(), peer)

		deleted, err := r.deleteExpiredPassive(t.Context(), []yagomodel.Hash{peer.Hash})
		if err != nil || len(deleted) != 0 || r.KnownPeerCount(t.Context()) != 1 {
			t.Fatalf("active eviction = deleted %#v error %v", deleted, err)
		}
	})

	t.Run("delete failure", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 8, 4)
		peer := internalSeed(t, "passive", "203.0.113.1")
		r.Discover(t.Context(), peer)
		engine.deleteErrors[peersBucket] = errors.New("delete failed")

		r.evictExpiredPassive(t.Context(), []yagomodel.Hash{peer.Hash})

		if r.KnownPeerCount(t.Context()) != 1 {
			t.Fatal("failed eviction removed the passive peer")
		}
	})

	t.Run("ownership rebuild failure", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 8, 4)
		peer := internalSeed(t, "passive", "203.0.113.1")
		r.Discover(t.Context(), peer)
		engine.scanErrors[peersBucket] = errors.New("scan failed")

		r.evictExpiredPassive(t.Context(), []yagomodel.Hash{peer.Hash})

		if r.KnownPeerCount(t.Context()) != 0 {
			t.Fatal("successful eviction was rolled back after ownership rebuild failure")
		}
	})
}
