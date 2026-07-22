package peerroster

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestTransportFailurePersistsCooldownAcrossRestartAndRetries(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.Discover(t.Context(), peer)
	r.ConfirmReachable(t.Context(), peer.Hash)
	r.ConfirmUnreachable(t.Context(), peer.Hash)
	if r.KnownPeerCount(t.Context()) != 1 || r.ReachablePeerCount(t.Context()) != 0 ||
		len(r.FreshestPeers(t.Context(), 8)) != 0 {
		t.Fatalf("cooling roster = known %d reachable %d candidates %d",
			r.KnownPeerCount(t.Context()), r.ReachablePeerCount(t.Context()),
			len(r.FreshestPeers(t.Context(), 8)))
	}
	reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
	if len(reopened.FreshestPeers(t.Context(), 8)) != 0 {
		t.Fatal("cooling peer became eligible after restart")
	}
	now = now.Add(peerPassiveRetryCooldown)
	candidates := reopened.FreshestPeers(t.Context(), 8)
	if len(candidates) != 1 || candidates[0].Hash != peer.Hash {
		t.Fatalf("retry candidates = %#v", candidates)
	}
	reopened.ConfirmReachable(t.Context(), peer.Hash)
	if reopened.ReachablePeerCount(t.Context()) != 1 {
		t.Fatal("successful retry did not promote the passive peer")
	}
}

func TestExpiredPassivePeerEvictsLazily(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), peer)
	r.ConfirmUnreachable(t.Context(), peer.Hash)
	now = now.Add(peerPassiveRetention + time.Second)
	if candidates := r.FreshestPeers(t.Context(), 8); len(candidates) != 0 {
		t.Fatalf("expired candidates = %#v", candidates)
	}
	if r.KnownPeerCount(t.Context()) != 1 {
		t.Fatal("read-only candidate selection mutated the roster")
	}
	r.PruneExpired(t.Context())
	if r.KnownPeerCount(t.Context()) != 0 {
		t.Fatal("expired passive peer remained persisted")
	}
}

func TestExpiredActivePeerLeavesReachableProjectionsAndPrunes(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), peer)
	if r.ReachablePeerCount(t.Context()) != 1 || len(r.FreshestPeers(t.Context(), 8)) != 1 {
		t.Fatal("reachable peer fixture was not active")
	}
	now = now.Add(peerPassiveRetention + time.Second)
	if r.ReachablePeerCount(t.Context()) != 0 || len(r.ReachablePeers(t.Context())) != 0 ||
		len(r.FreshestPeers(t.Context(), 8)) != 0 {
		t.Fatal("expired active peer remained in a reachable projection")
	}
	r.PruneExpired(t.Context())
	if r.KnownPeerCount(t.Context()) != 0 || r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatal("expired active peer survived pruning")
	}
}

func TestExpiredActivePeerDoesNotConsumeActiveCapacity(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 1)
	r.now = func() time.Time { return now }
	first := internalSeed(t, "first", "203.0.113.1")
	first.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), first)
	now = now.Add(peerPassiveRetention + time.Second)
	second := internalSeed(t, "second", "203.0.113.2")
	second.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), second)
	peers := r.ReachablePeers(t.Context())
	if r.ReachablePeerCount(t.Context()) != 1 || len(peers) != 1 ||
		peers[0].Hash != second.Hash {
		t.Fatalf("replacement active peers = %#v count=%d",
			peers, r.ReachablePeerCount(t.Context()))
	}
}

func TestDiscoveryPreservesAdvertisedFreshnessAndRejectsOutOfWindow(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	newer := timedInternalSeed(t, "newer", "203.0.113.1", now.Add(-time.Hour))
	older := timedInternalSeed(t, "older", "203.0.113.2", now.Add(-2*time.Hour))
	stale := timedInternalSeed(t, "stale", "203.0.113.3", now.Add(-25*time.Hour))
	future := timedInternalSeed(t, "future", "203.0.113.4", now.Add(time.Second))
	r.Discover(t.Context(), older, stale, future, newer)
	peers := r.FreshestPeers(t.Context(), 8)
	if len(peers) != 2 || peers[0].Hash != newer.Hash || peers[1].Hash != older.Hash {
		t.Fatalf("freshness-ranked peers = %#v", peers)
	}
	observations, known, _, err := r.PeerObservations(t.Context())
	if err != nil || known != 2 || !observations[0].LastSeen.Equal(now.Add(-time.Hour)) {
		t.Fatalf("observations = %#v known=%d error=%v", observations, known, err)
	}
}

func TestExactEndpointOwnershipRejectsAliasesAndVerifiedDisplacesUnverified(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	first := namedHostSeed(t, "first", "Peer.Example", 8090)
	second := namedHostSeed(t, "second", "peer.example", 8090)
	differentPort := namedHostSeed(t, "port", "peer.example", 8091)
	r.Discover(t.Context(), first, second, differentPort)
	if r.KnownPeerCount(t.Context()) != 2 {
		t.Fatalf("known endpoint owners = %d", r.KnownPeerCount(t.Context()))
	}
	verified := namedHostSeed(t, "verified", "peer.example", 8090)
	r.ObserveResponder(t.Context(), verified)
	if _, found := r.PeerByHash(t.Context(), first.Hash); !found {
		t.Fatal("displaced endpoint claim was not retained for later promotion")
	}
	if _, found := r.PeerByHash(t.Context(), verified.Hash); !found {
		t.Fatal("verified endpoint owner was not persisted")
	}
	for _, candidate := range r.FreshestPeers(t.Context(), 8) {
		if candidate.Hash == first.Hash {
			t.Fatal("displaced endpoint owner remained routable")
		}
	}
	reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
	claim := namedHostSeed(t, "claim", "PEER.EXAMPLE", 8090)
	reopened.Discover(t.Context(), claim)
	if _, found := reopened.PeerByHash(t.Context(), claim.Hash); found {
		t.Fatal("restart ownership rebuild admitted a conflicting claim")
	}
}

func TestPersistedEndpointCollisionProjectsOnlyWinnerThenPromotesLoser(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	winner := namedHostSeed(t, "winner", "peer.example", 8090)
	loser := namedHostSeed(t, "loser", "PEER.EXAMPLE", 8090)
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := r.putRosterEntry(tx, r.key(winner.Hash), rosterEntry{
			seed: winner, lastSeen: now, expiresAt: now.Add(time.Second), verified: true,
		}); err != nil {
			return fmt.Errorf("store collision winner: %w", err)
		}

		return r.putRosterEntry(tx, r.key(loser.Hash), rosterEntry{
			seed: loser, lastSeen: now.Add(-time.Minute),
			expiresAt: now.Add(time.Hour), verified: true,
		})
	}); err != nil {
		t.Fatal(err)
	}
	reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
	peers := reopened.FreshestPeers(t.Context(), 8)
	if len(peers) != 1 || peers[0].Hash != winner.Hash {
		t.Fatalf("collision winner projection = %#v", peers)
	}

	now = now.Add(2 * time.Second)
	reopened.ConfirmUnreachable(t.Context(), winner.Hash)
	peers = reopened.FreshestPeers(t.Context(), 8)
	if len(peers) != 1 || peers[0].Hash != loser.Hash {
		t.Fatalf("promoted endpoint owner = %#v", peers)
	}
}

func TestDiscoveryRefreshesNewerLegacyPeerButPreservesVerifiedEvidence(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	legacy := timedInternalSeed(t, "legacy", "203.0.113.1", now.Add(-23*time.Hour))
	raw := make([]byte, lastSeenWidth, lastSeenWidth+len(legacy.String()))
	binary.BigEndian.PutUint64(raw, uint64(now.Add(-23*time.Hour).UnixNano()))
	raw = append(raw, legacy.String()...)
	engine.buckets[peersBucket][string(r.key(legacy.Hash))] = raw

	reopened := reopenScriptedRoster(t, engine, func() time.Time { return now })
	fresh := timedInternalSeed(t, "legacy", "203.0.113.2", now.Add(-time.Hour))
	reopened.Discover(t.Context(), fresh)
	stored, found := reopened.PeerByHash(t.Context(), legacy.Hash)
	address, _ := stored.NetworkAddress()
	if !found || address != "203.0.113.2:8090" {
		t.Fatalf("refreshed legacy peer = %#v/%t", stored, found)
	}
	peers := reopened.FreshestPeers(t.Context(), 8)
	if len(peers) != 1 || peers[0].Hash != legacy.Hash {
		t.Fatalf("refreshed legacy candidates = %#v", peers)
	}

	verified := timedInternalSeed(t, "verified", "203.0.113.3", now.Add(-time.Hour))
	reopened.ObserveResponder(t.Context(), verified)
	replacement := timedInternalSeed(t, "verified", "203.0.113.4", now)
	reopened.Discover(t.Context(), replacement)
	stored, found = reopened.PeerByHash(t.Context(), verified.Hash)
	address, _ = stored.NetworkAddress()
	if !found || address != "203.0.113.3:8090" {
		t.Fatalf("verified peer was overwritten = %#v/%t", stored, found)
	}
}

func TestVirginResponderIsDurableButNeverRouted(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "virgin", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerVirgin)
	r.ObserveResponder(t.Context(), peer)
	if _, found := r.PeerByHash(t.Context(), peer.Hash); !found {
		t.Fatal("virgin responder was not persisted")
	}
	if r.ReachablePeerCount(t.Context()) != 0 || len(r.FreshestPeers(t.Context(), 8)) != 0 {
		t.Fatal("virgin responder entered active routing")
	}
}

func TestJuniorCallerDoesNotDowngradeVerifiedPeer(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	verified := internalSeed(t, "verified", "203.0.113.1")
	verified.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), verified)

	junior := internalSeed(t, "verified", "203.0.113.2")
	r.ObserveCaller(t.Context(), junior, yagomodel.PeerJunior)
	stored, found := r.PeerByHash(t.Context(), verified.Hash)
	address, _ := stored.NetworkAddress()
	if !found || address != "203.0.113.1:8090" || r.ReachablePeerCount(t.Context()) != 1 {
		t.Fatalf("verified peer after junior observation = %#v/%t reachable=%d",
			stored, found, r.ReachablePeerCount(t.Context()))
	}
	entries := r.SeedlistPeers(t.Context(), 8)
	if len(entries) != 1 || entries[0].Hash != verified.Hash {
		t.Fatalf("verified directory entries = %#v", entries)
	}
}

func TestDirectoryExportsVerifiedPassiveAndFindsNames(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	verified := internalSeed(t, "verified", "203.0.113.1")
	verified.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	verified.Name = yagomodel.Some("Example <Peer>")
	unverified := internalSeed(t, "gossip", "203.0.113.2")
	unverified.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	unverified.Name = yagomodel.Some("Example <Peer>")
	r.Discover(t.Context(), unverified)
	now = now.Add(time.Second)
	r.ObserveResponder(t.Context(), verified)
	r.ConfirmUnreachable(t.Context(), verified.Hash)
	seeds := r.SeedlistPeers(t.Context(), seedlistPeerLimit+1)
	if len(seeds) != 1 || seeds[0].Hash != verified.Hash {
		t.Fatalf("seedlist peers = %#v", seeds)
	}
	found, ok := r.PeerByName(t.Context(), "example _peer_")
	if !ok || found.Hash != verified.Hash {
		t.Fatalf("peer by name = %#v/%t", found, ok)
	}
	if spaced, found := r.PeerByName(t.Context(), " example _peer_ "); found || spaced.Hash != "" {
		t.Fatalf("whitespace-normalized peer = %#v/%t", spaced, found)
	}
}

func TestDirectoryPrefersActivePeerAndUsesBoundedCachedSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 1)
	r.now = func() time.Time { return now }
	active := internalSeed(t, "active", "203.0.113.1")
	active.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	active.Name = yagomodel.Some("Shared Name")
	r.ObserveResponder(t.Context(), active)
	now = now.Add(time.Second)
	passive := internalSeed(t, "passive", "203.0.113.2")
	passive.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	passive.Name = yagomodel.Some("Shared Name")
	r.ObserveResponder(t.Context(), passive)
	seeds := r.SeedlistPeers(t.Context(), 8)
	if len(seeds) != 2 || seeds[0].Hash != active.Hash || seeds[1].Hash != passive.Hash {
		t.Fatalf("active-first seedlist peers = %#v", seeds)
	}
	var scans atomic.Int32
	engine.scanObserver = func(name vault.Name) {
		if name == peersBucket {
			scans.Add(1)
		}
	}
	found, ok := r.PeerByName(t.Context(), "shared name")
	if !ok || found.Hash != active.Hash || scans.Load() != 0 {
		t.Fatalf("cached active peer lookup = %#v/%t scans=%d", found, ok, scans.Load())
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if peers := r.SeedlistPeers(canceled, 8); peers != nil {
		t.Fatalf("canceled seedlist peers = %#v", peers)
	}
	if peer, found := r.PeerByName(canceled, "shared name"); found || peer.Hash != "" {
		t.Fatalf("canceled peer lookup = %#v/%t", peer, found)
	}
}

func TestDiscoveryRefreshesExpiredVerifiedPeerForImmediateDirectoryUse(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, _ := openScriptedRoster(t, 8, 1)
	r.now = func() time.Time { return now }
	verified := timedInternalSeed(t, "verified", "203.0.113.1", now)
	verified.Name = yagomodel.Some("Verified Peer")
	r.ObserveResponder(t.Context(), verified)
	now = now.Add(peerPassiveRetention + time.Second)
	refreshed := timedInternalSeed(t, "verified", "203.0.113.2", now)
	refreshed.Name = yagomodel.Some("Verified Peer")
	r.Discover(t.Context(), refreshed)
	found, ok := r.PeerByName(t.Context(), "verified peer")
	address, _ := found.NetworkAddress()
	if !ok || found.Hash != verified.Hash || address != "203.0.113.2:8090" {
		t.Fatalf("refreshed verified directory peer = %#v/%t", found, ok)
	}
}

func TestRuntimeDiscoveryBatchTrimsDurableRosterToCapacity(t *testing.T) {
	const capacity = 3
	r, _ := openScriptedRoster(t, capacity, 1)
	seeds := make([]yagomodel.Seed, capacity+513)
	for position := range seeds {
		seeds[position] = internalSeed(t, stringHash(position), "203.0.113.1")
		seeds[position].PeerType = yagomodel.Some(yagomodel.PeerSenior)
		seeds[position].Port = yagomodel.Some(yagomodel.Port(10_000 + position))
	}
	r.Discover(t.Context(), seeds...)
	if r.KnownPeerCount(t.Context()) != capacity {
		t.Fatalf("runtime batch retained %d peers", r.KnownPeerCount(t.Context()))
	}
}

func TestLargeDiscoveryBatchUsesOneStorageCommit(t *testing.T) {
	r, engine := openScriptedRoster(t, 1024, 4)
	seeds := make([]yagomodel.Seed, 512)
	for position := range seeds {
		seeds[position] = internalSeed(t, stringHash(position), "203.0.113.1")
		seeds[position].PeerType = yagomodel.Some(yagomodel.PeerSenior)
		seeds[position].Port = yagomodel.Some(yagomodel.Port(10_000 + position))
	}
	before := engine.updates.Load()
	r.Discover(t.Context(), seeds...)
	if commits := engine.updates.Load() - before; commits != 1 {
		t.Fatalf("discovery storage commits = %d", commits)
	}
	if r.KnownPeerCount(t.Context()) != len(seeds) {
		t.Fatalf("known peers = %d", r.KnownPeerCount(t.Context()))
	}
}

func TestCanceledLargeDiscoveryBatchDoesNotWrite(t *testing.T) {
	r, engine := openScriptedRoster(t, 4096, 4)
	seeds := make([]yagomodel.Seed, 4096)
	for position := range seeds {
		seeds[position] = internalSeed(t, stringHash(position), "203.0.113.1")
		seeds[position].PeerType = yagomodel.Some(yagomodel.PeerSenior)
		seeds[position].Port = yagomodel.Some(yagomodel.Port(10_000 + position))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := engine.updates.Load()
	r.Discover(ctx, seeds...)
	if engine.updates.Load() != before || r.KnownPeerCount(t.Context()) != 0 {
		t.Fatalf("canceled discovery writes/peers = %d/%d",
			engine.updates.Load()-before, r.KnownPeerCount(t.Context()))
	}
}

func TestCandidateSelectionDoesNotWaitForMaintenanceWrites(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	peer := timedInternalSeed(t, "peer", "203.0.113.1", now)
	r.Discover(t.Context(), peer)
	if len(r.FreshestPeers(t.Context(), 8)) != 1 {
		t.Fatal("candidate snapshot was not primed")
	}
	now = now.Add(peerPassiveRetention + time.Second)
	started := make(chan struct{}, 1)
	blocked := make(chan struct{})
	engine.updateStarted = started
	engine.updateBlock = blocked
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if peers := r.FreshestPeers(ctx, 8); len(peers) != 0 {
		t.Fatalf("expired candidates = %#v", peers)
	}
	if elapsed := time.Since(begin); elapsed >= 50*time.Millisecond {
		t.Fatalf("read-only candidate selection took %s", elapsed)
	}
	select {
	case <-started:
		t.Fatal("candidate selection attempted a maintenance write")
	default:
	}
}

func TestMembershipMutationWaitHonorsContext(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	if !r.acquireMembership(t.Context()) {
		t.Fatal("membership permit was unavailable")
	}
	defer r.releaseMembership()
	actions := map[string]func(context.Context){
		"discover": func(ctx context.Context) { r.Discover(ctx, peer) },
		"caller": func(ctx context.Context) {
			r.ObserveCaller(ctx, peer, yagomodel.PeerSenior)
		},
		"responder":    func(ctx context.Context) { r.ObserveResponder(ctx, peer) },
		"potential":    func(ctx context.Context) { r.ObservePotential(ctx, peer) },
		"reachable":    func(ctx context.Context) { r.ConfirmReachable(ctx, peer.Hash) },
		"unreachable":  func(ctx context.Context) { r.ConfirmUnreachable(ctx, peer.Hash) },
		"remote index": func(ctx context.Context) { r.RejectRemoteIndex(ctx, peer) },
		"expiry": func(ctx context.Context) {
			r.evictExpiredPassive(ctx, []yagomodel.Hash{peer.Hash})
		},
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			done := make(chan struct{})
			go func() {
				action(ctx)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
				t.Fatal("membership mutation ignored context cancellation")
			}
		})
	}
}

func TestStartupCapacityUsesOneValueScanAndKeepsFreshestOutsideKeyPrefix(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	const (
		capacity = 3
		total    = capacity + 513
	)
	r, engine := openScriptedRoster(t, total, 1)
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for position := 1; position < total; position++ {
			seed := internalSeed(t, stringHash(position), "203.0.113.1")
			seed.PeerType = yagomodel.Some(yagomodel.PeerSenior)
			seed.Port = yagomodel.Some(yagomodel.Port(10_000 + position))
			if err := r.putRosterEntry(tx, r.key(seed.Hash), rosterEntry{
				seed: seed, lastSeen: now.Add(-time.Duration(position) * time.Minute),
				expiresAt: now.Add(time.Hour), verified: true,
			}); err != nil {
				return fmt.Errorf("store startup capacity peer: %w", err)
			}
		}
		winner := internalSeed(t, "ZZZZZZZZZZZZ", "203.0.113.2")
		winner.PeerType = yagomodel.Some(yagomodel.PeerSenior)

		return r.putRosterEntry(tx, r.key(winner.Hash), rosterEntry{
			seed: winner, lastSeen: now, expiresAt: now.Add(time.Hour), verified: true,
		})
	}); err != nil {
		t.Fatal(err)
	}
	var scans atomic.Int32
	engine.scanObserver = func(name vault.Name) {
		if name == peersBucket {
			scans.Add(1)
		}
	}
	engine.keyPages.Store(0)
	reopened := reopenScriptedRosterWithCaps(
		t,
		engine,
		func() time.Time { return now },
		capacity,
		1,
	)
	if scans.Load() != 1 || engine.keyPages.Load() != 4 {
		t.Fatalf("startup value scans/key pages = %d/%d", scans.Load(), engine.keyPages.Load())
	}
	if reopened.KnownPeerCount(t.Context()) != capacity {
		t.Fatalf("retained peer count = %d", reopened.KnownPeerCount(t.Context()))
	}
	if _, found := reopened.PeerByHash(t.Context(), internalHashFor("ZZZZZZZZZZZZ")); !found {
		t.Fatal("freshest peer outside the first key page was discarded")
	}
}

func TestStartupCapacityPreservesVerifiedEndpointOwner(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 2, 1)
	verified := namedHostSeed(t, "verified", "peer.example", 8090)
	unverified := namedHostSeed(t, "unverified", "PEER.EXAMPLE", 8090)
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := r.putRosterEntry(tx, r.key(verified.Hash), rosterEntry{
			seed: verified, lastSeen: now.Add(-time.Hour),
			expiresAt: now.Add(time.Hour), verified: true,
		}); err != nil {
			return fmt.Errorf("store verified endpoint owner: %w", err)
		}

		return r.putRosterEntry(tx, r.key(unverified.Hash), rosterEntry{
			seed: unverified, lastSeen: now,
			expiresAt: now.Add(time.Hour), verified: false,
		})
	}); err != nil {
		t.Fatal(err)
	}
	reopened := reopenScriptedRosterWithCaps(t, engine, func() time.Time { return now }, 1, 1)
	if _, found := reopened.PeerByHash(t.Context(), verified.Hash); !found {
		t.Fatal("verified endpoint owner was discarded at startup")
	}
	if _, found := reopened.PeerByHash(t.Context(), unverified.Hash); found {
		t.Fatal("unverified endpoint claimant survived capacity trimming")
	}
}

func TestStartupCapacityEvictsExpiredBeforeCurrentPeer(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	r, engine := openScriptedRoster(t, 2, 1)
	expired := timedInternalSeed(t, "expired", "203.0.113.1", now)
	current := timedInternalSeed(t, "current", "203.0.113.2", now.Add(-time.Hour))
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := r.putRosterEntry(tx, r.key(expired.Hash), rosterEntry{
			seed: expired, lastSeen: now, expiresAt: now.Add(-time.Second), verified: true,
		}); err != nil {
			return fmt.Errorf("store expired startup peer: %w", err)
		}

		return r.putRosterEntry(tx, r.key(current.Hash), rosterEntry{
			seed: current, lastSeen: now.Add(-time.Hour),
			expiresAt: now.Add(time.Hour), verified: true,
		})
	}); err != nil {
		t.Fatal(err)
	}
	reopened := reopenScriptedRosterWithCaps(t, engine, func() time.Time { return now }, 1, 1)
	if _, found := reopened.PeerByHash(t.Context(), current.Hash); !found {
		t.Fatal("current peer was discarded before an expired peer")
	}
	if _, found := reopened.PeerByHash(t.Context(), expired.Hash); found {
		t.Fatal("expired peer survived startup capacity trimming")
	}
}

func TestStartupZeroCapacityEvictsLegacyRows(t *testing.T) {
	r, engine := openScriptedRoster(t, 1, 0)
	peer := internalSeed(t, "peer", "203.0.113.1")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(peer.Hash), rosterEntry{
			seed: peer, lastSeen: time.Now(), expiresAt: time.Now().Add(time.Hour),
		})
	}); err != nil {
		t.Fatal(err)
	}
	reopened := reopenScriptedRosterWithCaps(t, engine, time.Now, 0, 0)
	if reopened.KnownPeerCount(t.Context()) != 0 {
		t.Fatalf("zero-cap roster retained %d peers", reopened.KnownPeerCount(t.Context()))
	}
}

func reopenScriptedRoster(
	t *testing.T,
	engine *scriptedEngine,
	now func() time.Time,
) *roster {
	return reopenScriptedRosterWithCaps(t, engine, now, 8, 4)
}

func reopenScriptedRosterWithCaps(
	t *testing.T,
	engine *scriptedEngine,
	now func() time.Time,
	reservoirCap int,
	activeCap int,
) *roster {
	t.Helper()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(
		t.Context(), storage, internalHashFor("local"), now,
		Capacity{Reservoir: reservoirCap, Active: activeCap},
	)
	if err != nil {
		t.Fatal(err)
	}

	return opened.(*roster)
}

func timedInternalSeed(
	t *testing.T,
	hash string,
	ip string,
	seen time.Time,
) yagomodel.Seed {
	t.Helper()
	seed := internalSeed(t, hash, ip)
	seed.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(seen))

	return seed
}

func namedHostSeed(t *testing.T, hash, hostname string, port int) yagomodel.Seed {
	t.Helper()
	host, err := yagomodel.ParseHost(hostname)
	if err != nil {
		t.Fatal(err)
	}

	return yagomodel.Seed{
		Hash:     internalHashFor(hash),
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(yagomodel.Port(port)),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
	}
}

func stringHash(position int) string {
	return fmt.Sprintf("%012d", position)
}
