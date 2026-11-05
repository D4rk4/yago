package peerroster

import (
	"bytes"
	"encoding/binary"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRosterWrittenWithoutAnsweredEndpointsReopensOnAdvertisedEndpoints(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.ObserveResponder(t.Context(), peer)
	delete(engine.buckets, answeredEndpointsBucket)

	reopened := reopenScriptedRoster(t, engine, r.now)

	if err := reopened.vault.View(t.Context(), func(tx *vault.Txn) error {
		stored, found, err := reopened.getRosterEntry(tx, reopened.key(peer.Hash))
		if err != nil || !found || !stored.verified || stored.seed.String() != peer.String() {
			t.Fatalf("legacy peer row = %#v/%t/%v", stored, found, err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertNoAnsweredEndpoint(t, reopened, peer.Hash)
	advertised, err := peer.ProtocolEndpoints("/yacy/hello.html", false)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := peerwire.ContactEndpoints(
		peer, "/yacy/hello.html", false, yagomodel.None[peerwire.Endpoint](),
	)
	if err != nil || !slices.EqualFunc(selected, advertised, func(a, b *url.URL) bool {
		return a.String() == b.String()
	}) {
		t.Fatalf("legacy selection = %v/%v, want %v", selected, err, advertised)
	}
}

func TestPeerRowsBesideAnsweredEndpointsRemainReadableByV0020(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	peer := internalSeed(t, "peer", "203.0.113.1")
	observeAnsweredAt(t, r, peer, "http://203.0.113.1:8090")
	key := string(r.key(peer.Hash))

	raw := engine.buckets[peersBucket][key]
	expected := make([]byte, lastSeenWidth, lastSeenWidth+len(peer.String()))
	binary.BigEndian.PutUint64(expected, uint64(now.UnixNano()))
	expected = append(expected, peer.String()...)
	if !bytes.Equal(raw, expected) {
		t.Fatalf("peer row = %x, want exact v0.0.20 row %x", raw, expected)
	}
	legacy, err := decodeRosterEntryWithV0020Algorithm(raw)
	if err != nil || legacy.seed.String() != peer.String() || !legacy.lastSeen.Equal(now) {
		t.Fatalf("v0.0.20 row = %#v/%v", legacy, err)
	}
	if !bytes.HasPrefix(
		engine.buckets[answeredEndpointsBucket][key],
		[]byte(answeredEndpointMagic),
	) {
		t.Fatal("answered endpoint row is missing its record format marker")
	}
	if len(engine.buckets[peerLifecyclesBucket]) != 1 {
		t.Fatalf("lifecycle rows = %d", len(engine.buckets[peerLifecyclesBucket]))
	}
}

func TestAnsweredEndpointLeftByPartialDeletionNeedsFreshVerification(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	answered := answeredOrigin(t, "http://203.0.113.1:8090")
	r.ObserveAnsweredResponder(t.Context(), peer, answered)
	key := string(r.key(peer.Hash))
	delete(engine.buckets[peersBucket], key)
	delete(engine.buckets[peerLifecyclesBucket], key)

	assertNoAnsweredEndpoint(t, r, peer.Hash)
	r.Discover(t.Context(), peer)
	assertNoAnsweredEndpoint(t, r, peer.Hash)
	r.ObserveResponder(t.Context(), peer)
	assertAnsweredEndpoint(t, r, peer.Hash, answered)
}
