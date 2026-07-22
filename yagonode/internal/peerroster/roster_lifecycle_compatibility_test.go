package peerroster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type v0020RosterEntry struct {
	seed     yagomodel.Seed
	lastSeen time.Time
}

func decodeRosterEntryWithV0020Algorithm(raw []byte) (v0020RosterEntry, error) {
	if len(raw) < lastSeenWidth {
		return v0020RosterEntry{}, fmt.Errorf("short record")
	}
	nanos := int64(binary.BigEndian.Uint32(raw[:4]))<<32 |
		int64(binary.BigEndian.Uint32(raw[4:lastSeenWidth]))
	seed, err := yagomodel.ParseSeed(context.Background(), string(raw[lastSeenWidth:]))
	if err != nil {
		return v0020RosterEntry{}, fmt.Errorf("parse v0.0.20 roster seed: %w", err)
	}

	return v0020RosterEntry{seed: seed, lastSeen: time.Unix(0, nanos)}, nil
}

func TestCandidateRosterRowsRemainReadableByV0020(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	entry := rosterEntry{
		seed:       peer,
		lastSeen:   now,
		retryAfter: now.Add(time.Minute),
		expiresAt:  now.Add(time.Hour),
		verified:   true,
	}
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(peer.Hash), entry)
	}); err != nil {
		t.Fatal(err)
	}
	raw := engine.buckets[peersBucket][string(r.key(peer.Hash))]
	expected := make([]byte, lastSeenWidth, lastSeenWidth+len(peer.String()))
	binary.BigEndian.PutUint64(expected, uint64(now.UnixNano()))
	expected = append(expected, peer.String()...)
	if !bytes.Equal(raw, expected) {
		t.Fatalf("candidate peer row = %x, want exact v0.0.20 row %x", raw, expected)
	}
	legacy, err := decodeRosterEntryWithV0020Algorithm(raw)
	if err != nil {
		t.Fatalf("v0.0.20 decoder rejected candidate row: %v", err)
	}
	if legacy.seed.String() != peer.String() || !legacy.lastSeen.Equal(now) {
		t.Fatalf("v0.0.20 row = %#v", legacy)
	}
	if bytes.Contains(raw, rosterLifecycleMagic) || legacy.seed.String() != peer.String() {
		t.Fatalf("peer row exposed lifecycle metadata: %q", raw)
	}
	if len(engine.buckets[peerLifecyclesBucket]) != 1 {
		t.Fatalf("lifecycle rows = %d", len(engine.buckets[peerLifecyclesBucket]))
	}

	reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
	if err := reopened.vault.View(t.Context(), func(tx *vault.Txn) error {
		stored, found, err := reopened.getRosterEntry(tx, reopened.key(peer.Hash))
		if err != nil {
			return err
		}
		if !found || !stored.retryAfter.Equal(entry.retryAfter) ||
			!stored.expiresAt.Equal(entry.expiresAt) || !stored.verified {
			t.Fatalf("reopened lifecycle = %#v/%t", stored, found)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateRosterZeroTimeRetainsV0020Bytes(t *testing.T) {
	peer := internalSeed(t, "peer", "203.0.113.1")
	raw, err := (rosterEntryCodec{}).Encode(rosterEntry{seed: peer})
	if err != nil {
		t.Fatal(err)
	}
	expected := make([]byte, lastSeenWidth, lastSeenWidth+len(peer.String()))
	binary.BigEndian.PutUint64(expected, uint64((time.Time{}).UnixNano()))
	expected = append(expected, peer.String()...)
	if !bytes.Equal(raw, expected) {
		t.Fatalf("zero-time peer row = %x, want %x", raw, expected)
	}
}

func TestV0020PrimaryOnlyRowUsesConservativeLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.peers.Put(tx, r.key(peer.Hash), rosterEntry{
			seed: peer, lastSeen: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[peerLifecyclesBucket]) != 0 {
		t.Fatal("v0.0.20 primary-only fixture wrote lifecycle metadata")
	}
	assertConservativeLifecycle(t, r, peer.Hash, now)

	reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
	assertConservativeLifecycle(t, reopened, peer.Hash, now)
	if stored, found := reopened.PeerByHash(t.Context(), peer.Hash); !found ||
		stored.String() != peer.String() {
		t.Fatalf("v0.0.20 primary-only peer = %q/%t", stored.String(), found)
	}
}

func TestV0020MutationInvalidatesCandidateLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	for name, mutate := range map[string]func(*rosterEntry){
		"timestamp": func(replacement *rosterEntry) {
			replacement.lastSeen = now.Add(time.Minute)
		},
		"seed": func(replacement *rosterEntry) {
			replacement.seed = internalSeed(t, "peer", "203.0.113.2")
			replacement.seed.PeerType = yagomodel.Some(yagomodel.PeerSenior)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, engine := openScriptedRoster(t, 8, 4)
			peer := internalSeed(t, "peer", "203.0.113.1")
			peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
			entry := rosterEntry{
				seed:       peer,
				lastSeen:   now,
				retryAfter: now.Add(time.Hour),
				expiresAt:  now.Add(2 * time.Hour),
				verified:   true,
			}
			if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return r.putRosterEntry(tx, r.key(peer.Hash), entry)
			}); err != nil {
				t.Fatal(err)
			}

			replacement := entry
			mutate(&replacement)
			raw, err := (rosterEntryCodec{}).Encode(replacement)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRosterEntryWithV0020Algorithm(raw); err != nil {
				t.Fatalf("v0.0.20 replacement row is unreadable: %v", err)
			}
			engine.buckets[peersBucket][string(r.key(peer.Hash))] = raw
			assertConservativeLifecycle(t, r, peer.Hash, replacement.lastSeen)

			reopened := reopenScriptedRoster(
				t,
				engine,
				func() time.Time { return replacement.lastSeen },
			)
			assertConservativeLifecycle(t, reopened, peer.Hash, replacement.lastSeen)
			if _, found := engine.buckets[peerLifecyclesBucket][string(r.key(peer.Hash))]; found {
				t.Fatal("stale lifecycle survived restart cleanup")
			}
		})
	}
}

func TestRosterLifecycleIntegrityRejectsValidStateCorruption(t *testing.T) {
	for name, corrupt := range map[string]func([]byte){
		"retry timestamp": func(raw []byte) {
			raw[len(rosterLifecycleMagic)+sha256.Size+7] ^= 1
		},
		"verification state": func(raw []byte) {
			raw[len(raw)-1] = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Unix(100, 0)
			r, engine := openScriptedRoster(t, 8, 4)
			peer := internalSeed(t, "peer", "203.0.113.1")
			entry := rosterEntry{
				seed:       peer,
				lastSeen:   now,
				retryAfter: now.Add(time.Hour),
				expiresAt:  now.Add(2 * time.Hour),
			}
			if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
				return r.putRosterEntry(tx, r.key(peer.Hash), entry)
			}); err != nil {
				t.Fatal(err)
			}
			key := string(r.key(peer.Hash))
			corrupt(engine.buckets[peerLifecyclesBucket][key])
			if _, err := (rosterLifecycleCodec{}).Decode(
				engine.buckets[peerLifecyclesBucket][key],
			); err != nil {
				t.Fatalf("corruption was not structurally valid: %v", err)
			}
			assertConservativeLifecycle(t, r, peer.Hash, now)

			reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
			assertConservativeLifecycle(t, reopened, peer.Hash, now)
			if _, found := engine.buckets[peerLifecyclesBucket][key]; found {
				t.Fatal("corrupt lifecycle survived restart cleanup")
			}
		})
	}
}

func assertConservativeLifecycle(
	t *testing.T,
	r *roster,
	peer yagomodel.Hash,
	lastSeen time.Time,
) {
	t.Helper()
	if err := r.vault.View(t.Context(), func(tx *vault.Txn) error {
		stored, found, err := r.getRosterEntry(tx, r.key(peer))
		if err != nil {
			return err
		}
		if !found || stored.verified || !stored.retryAfter.IsZero() ||
			!stored.expiresAt.Equal(lastSeen.Add(peerPassiveRetention)) {
			t.Fatalf("downgrade fallback lifecycle = %#v/%t", stored, found)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRosterLifecycleCleanupRemovesOrphansWithinItsBound(t *testing.T) {
	r, engine := openScriptedRoster(t, rosterLifecycleCleanupLimit, 4)
	lastSeen := time.Unix(100, 0)
	orphanKey := vault.Key("zzzzzzzzzzzz")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for position := range rosterLifecycleCleanupLimit {
			peer := internalSeed(t, stringHash(position), "203.0.113.1")
			entry := rosterEntry{
				seed:      peer,
				lastSeen:  lastSeen,
				expiresAt: lastSeen.Add(peerPassiveRetention),
			}
			if err := r.putRosterEntry(tx, r.key(peer.Hash), entry); err != nil {
				return err
			}
		}
		orphan, err := rosterLifecycleFor(rosterEntry{
			seed:     internalSeed(t, "orphan", "203.0.113.2"),
			lastSeen: lastSeen,
		})
		if err != nil {
			return err
		}

		return r.lifecycles.Put(tx, orphanKey, orphan)
	}); err != nil {
		t.Fatal(err)
	}

	first := reopenScriptedRosterWithCaps(
		t,
		engine,
		func() time.Time { return lastSeen },
		rosterLifecycleCleanupLimit,
		4,
	)
	if rows := len(engine.buckets[peerLifecyclesBucket]); rows != rosterLifecycleCleanupLimit+1 {
		t.Fatalf("lifecycle rows after bounded cleanup = %d", rows)
	}
	if _, found := engine.buckets[peerLifecyclesBucket][string(orphanKey)]; !found {
		t.Fatal("bounded cleanup inspected beyond its limit")
	}
	if first.KnownPeerCount(t.Context()) != rosterLifecycleCleanupLimit {
		t.Fatalf("valid peers after bounded cleanup = %d", first.KnownPeerCount(t.Context()))
	}

	second := reopenScriptedRosterWithCaps(
		t,
		engine,
		func() time.Time { return lastSeen },
		rosterLifecycleCleanupLimit,
		4,
	)
	if rows := len(engine.buckets[peerLifecyclesBucket]); rows != rosterLifecycleCleanupLimit {
		t.Fatalf("continued lifecycle cleanup retained %d rows", rows)
	}
	if _, found := engine.buckets[peerLifecyclesBucket][string(orphanKey)]; found {
		t.Fatal("continued lifecycle cleanup did not reach the late orphan")
	}
	if _, found := second.PeerByHash(t.Context(), internalHashFor(stringHash(0))); !found {
		t.Fatal("continued lifecycle cleanup discarded a valid peer")
	}
}

func TestRosterLifecycleCodecRejectsMalformedRecords(t *testing.T) {
	valid, err := (rosterLifecycleCodec{}).Encode(rosterLifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	badMagic := append([]byte(nil), valid...)
	badMagic[0] = 'X'
	badVerification := append([]byte(nil), valid...)
	badVerification[len(badVerification)-1] = 2
	for name, raw := range map[string][]byte{
		"truncated":         valid[:len(valid)-1],
		"oversized":         append(append([]byte(nil), valid...), 0),
		"identity":          badMagic,
		"verification":      badVerification,
		"empty":             nil,
		"header only":       append([]byte(nil), rosterLifecycleMagic...),
		"hostile expansion": bytes.Repeat([]byte{'A'}, 1<<20),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := (rosterLifecycleCodec{}).Decode(raw); err == nil {
				t.Fatal("malformed lifecycle accepted")
			}
		})
	}
}

func TestRosterLifecycleCleanupCursorCodecBoundsAndCopies(t *testing.T) {
	cursor := vault.Key("peer")
	raw, err := (rosterLifecycleCleanupCursorCodec{}).Encode(cursor)
	if err != nil {
		t.Fatal(err)
	}
	cursor[0] = 'x'
	decoded, err := (rosterLifecycleCleanupCursorCodec{}).Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'y'
	if string(decoded) != "peer" {
		t.Fatalf("decoded cursor = %q", decoded)
	}
	for name, value := range map[string][]byte{
		"empty":     nil,
		"oversized": make([]byte, rosterLifecycleCleanupCursorMaximumWidth+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := (rosterLifecycleCleanupCursorCodec{}).Encode(value); err == nil {
				t.Fatal("invalid cursor encoded")
			}
			if _, err := (rosterLifecycleCleanupCursorCodec{}).Decode(value); err == nil {
				t.Fatal("invalid cursor decoded")
			}
		})
	}
}

func TestLegacyRosterMagicCollisionRemainsLegacy(t *testing.T) {
	seed := internalSeed(t, "peer", "203.0.113.1")
	raw := make([]byte, lastSeenWidth, lastSeenWidth+len(seed.String()))
	copy(raw, []byte{'Y', 'P', 'R', '2'})
	binary.BigEndian.PutUint32(raw[4:8], 1)
	raw = append(raw, seed.String()...)

	entry, err := (rosterEntryCodec{}).Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if entry.seed.Hash != seed.Hash || entry.verified || !entry.retryAfter.IsZero() {
		t.Fatalf("magic-collision entry = %#v", entry)
	}
}

func TestMalformedOrphanLifecycleIsDiscardedOnRosterOpen(t *testing.T) {
	_, engine := openScriptedRoster(t, 8, 4)
	engine.buckets[peerLifecyclesBucket]["orphan"] = []byte("bad")
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[peerLifecyclesBucket]) != 0 {
		t.Fatal("malformed lifecycle survived roster open")
	}
}
