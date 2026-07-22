package peerroster

import (
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestUnreachableTransitionHandlesDeleteAndCooldownStorageFailures(t *testing.T) {
	t.Run("expired delete", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
		r, engine := openScriptedRoster(t, 8, 4)
		r.now = func() time.Time { return now }
		peer := internalSeed(t, "peer", "203.0.113.1")
		peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
		r.ObserveResponder(t.Context(), peer)
		now = now.Add(peerPassiveRetention + time.Second)
		engine.deleteErrors[peersBucket] = errors.New("delete failed")

		r.ConfirmUnreachable(t.Context(), peer.Hash)

		if r.KnownPeerCount(t.Context()) != 1 {
			t.Fatal("failed expired-peer delete removed the peer")
		}
	})

	t.Run("cooldown write", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 8, 4)
		peer := internalSeed(t, "peer", "203.0.113.1")
		r.Discover(t.Context(), peer)
		engine.putErrors[peersBucket] = errors.New("put failed")

		r.ConfirmUnreachable(t.Context(), peer.Hash)

		if r.KnownPeerCount(t.Context()) != 1 {
			t.Fatal("failed cooldown write removed the peer")
		}
	})
}

func TestExpiredUnreachablePeerRemainsDeletedWhenOwnershipRebuildFails(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), peer)
	now = now.Add(peerPassiveRetention + time.Second)
	engine.scanErrors[peersBucket] = errors.New("scan failed")

	r.ConfirmUnreachable(t.Context(), peer.Hash)

	if r.KnownPeerCount(t.Context()) != 0 {
		t.Fatal("deleted peer returned after ownership rebuild failure")
	}
}
