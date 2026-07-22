package peerroster_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

func TestDiscoverKeepsSeniorsAndDropsJuniors(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	addressless := seniorSeed(t, "noaddress", "", 0)
	junior := seniorSeed(t, "junior", "203.0.113.2", 8090)
	junior.PeerType = yagomodel.Some(yagomodel.PeerJunior)
	roster.Discover(ctx, senior, addressless, junior)

	targets := hashes(roster.FreshestPeers(ctx, 4))
	if _, ok := targets[senior.Hash]; !ok {
		t.Fatalf("senior missing from greet targets: %v", targets)
	}
	if _, ok := targets[junior.Hash]; ok {
		t.Fatalf("junior should have been dropped: %v", targets)
	}
	if _, ok := targets[addressless.Hash]; ok {
		t.Fatalf("addressless seed should have been dropped: %v", targets)
	}
}

func TestRosterRejectsSelfAcrossMutationAndProjectionPaths(t *testing.T) {
	ctx := t.Context()
	roster := openRoster(t, 8, 4)
	self := seniorSeed(t, "local", "203.0.113.1", 8090)
	self.Flags = yagomodel.Some(
		yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
	)

	roster.Discover(ctx, self)
	roster.ObserveCaller(ctx, self, yagomodel.PeerSenior)
	roster.ObserveResponder(ctx, self)
	roster.ConfirmReachable(ctx, self.Hash)
	roster.RejectRemoteIndex(ctx, self)
	roster.ConfirmUnreachable(ctx, self.Hash)

	if roster.KnownPeerCount(ctx) != 0 || roster.ReachablePeerCount(ctx) != 0 {
		t.Fatalf(
			"self counts = known %d reachable %d, want zero",
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}
	if _, found := roster.PeerByHash(ctx, self.Hash); found {
		t.Fatal("self resolved by hash")
	}
	if peers := roster.ReachablePeers(ctx); len(peers) != 0 {
		t.Fatalf("reachable self peers = %#v, want none", peers)
	}
	if peers := roster.FreshestPeers(ctx, 8); len(peers) != 0 {
		t.Fatalf("freshest self peers = %#v, want none", peers)
	}
	reader := roster.(peerroster.ObservationReader)
	observations, known, reachable, err := reader.PeerObservations(ctx)
	if err != nil || len(observations) != 0 || known != 0 || reachable != 0 {
		t.Fatalf(
			"self observations = %#v known/reachable %d/%d error %v",
			observations,
			known,
			reachable,
			err,
		)
	}
	if _, found, err := reader.PeerObservation(ctx, self.Hash); err != nil || found {
		t.Fatalf("self observation = found %v error %v, want absent", found, err)
	}
}

func TestRosterDoesNotTreatSharedEndpointAsSelf(t *testing.T) {
	roster := openRoster(t, 8, 4)
	peer := seniorSeed(t, "neighbor", "203.0.113.1", 8090)

	roster.Discover(t.Context(), peer)

	stored, found := roster.PeerByHash(t.Context(), peer.Hash)
	if !found || stored.Hash != peer.Hash || roster.KnownPeerCount(t.Context()) != 1 {
		t.Fatalf("shared-endpoint peer = %#v found %v", stored, found)
	}
}

func TestVerifiedResponderRefreshesMetadataAndResistsIndirectDowngrade(t *testing.T) {
	roster := openRoster(t, 8, 4)
	stale := seniorSeed(t, "peer", "203.0.113.1", 8090)
	stale.Name = yagomodel.Some("stale-peer")
	stale.RWICount = yagomodel.Some(17)
	roster.Discover(t.Context(), stale)
	reader := roster.(peerroster.ObservationReader)
	before, found, err := reader.PeerObservation(t.Context(), stale.Hash)
	if err != nil || !found {
		t.Fatalf("stale observation = %#v found %v error %v", before, found, err)
	}

	current := stale.Copy()
	current.Name = yagomodel.Some("current-peer")
	current.RWICount = yagomodel.Some(8_363_840)
	roster.ObserveResponder(t.Context(), current)
	afterResponse, found, err := reader.PeerObservation(t.Context(), current.Hash)
	if err != nil || !found || !afterResponse.LastSeen.After(before.LastSeen) {
		t.Fatalf(
			"responder observation = %#v found %v error %v, want newer than %#v",
			afterResponse,
			found,
			err,
			before,
		)
	}
	roster.Discover(t.Context(), stale)
	afterGossip, found, err := reader.PeerObservation(t.Context(), current.Hash)
	if err != nil || !found || afterGossip.LastSeen != afterResponse.LastSeen {
		t.Fatalf(
			"gossip observation = %#v found %v error %v, want unchanged %#v",
			afterGossip,
			found,
			err,
			afterResponse,
		)
	}

	stored, found := roster.PeerByHash(t.Context(), current.Hash)
	name, _ := stored.Name.Get()
	rwi, _ := stored.RWICount.Get()
	if !found || name != "current-peer" || rwi != 8_363_840 {
		t.Fatalf("refreshed peer = %#v found %v", stored, found)
	}
	reachable := roster.ReachablePeers(t.Context())
	if len(reachable) != 1 || reachable[0].Hash != current.Hash {
		t.Fatalf("reachable responders = %#v, want refreshed peer", reachable)
	}
}

func TestCallerObservationRetainsPromotesAndDoesNotDowngradePeer(t *testing.T) {
	ctx := t.Context()
	roster := openRoster(t, 8, 4)
	caller := seniorSeed(t, "caller", "203.0.113.2", 8090)
	caller.Name = yagomodel.Some("first")

	roster.ObserveCaller(ctx, caller, yagomodel.PeerJunior)
	stored, found := roster.PeerByHash(ctx, caller.Hash)
	classification, classified := stored.PeerType.Get()
	if !found || !classified || classification != yagomodel.PeerJunior ||
		roster.KnownPeerCount(ctx) != 1 || roster.ReachablePeerCount(ctx) != 0 {
		t.Fatalf(
			"junior observation = found %v type %q/%v known/reachable %d/%d",
			found,
			classification,
			classified,
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}
	if candidates := roster.FreshestPeers(ctx, 8); len(candidates) != 0 {
		t.Fatalf("junior candidates = %#v, want none", candidates)
	}

	caller.Name = yagomodel.Some("updated")
	roster.ObserveCaller(ctx, caller, yagomodel.PeerSenior)
	stored, found = roster.PeerByHash(ctx, caller.Hash)
	classification, classified = stored.PeerType.Get()
	name, _ := stored.Name.Get()
	if !found || !classified || classification != yagomodel.PeerSenior || name != "updated" ||
		roster.KnownPeerCount(ctx) != 1 || roster.ReachablePeerCount(ctx) != 1 {
		t.Fatalf(
			"senior observation = %#v found %v type %q/%v known/reachable %d/%d",
			stored,
			found,
			classification,
			classified,
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}
	if candidates := roster.FreshestPeers(ctx, 8); len(candidates) != 1 ||
		candidates[0].Hash != caller.Hash {
		t.Fatalf("senior candidates = %#v, want promoted caller", candidates)
	}

	roster.ObserveCaller(ctx, caller, yagomodel.PeerJunior)
	roster.ConfirmReachable(ctx, caller.Hash)
	roster.ConfirmUnreachable(ctx, caller.Hash)
	stored, found = roster.PeerByHash(ctx, caller.Hash)
	classification, classified = stored.PeerType.Get()
	if !found || !classified || classification != yagomodel.PeerSenior ||
		roster.KnownPeerCount(ctx) != 1 || roster.ReachablePeerCount(ctx) != 0 {
		t.Fatalf(
			"retained verified observation = found %v type %q/%v known/reachable %d/%d",
			found,
			classification,
			classified,
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}
	if candidates := roster.FreshestPeers(ctx, 8); len(candidates) != 0 {
		t.Fatalf("cooling senior candidates = %#v, want none", candidates)
	}
}

func TestCallerObservationRejectsAddresslessJunior(t *testing.T) {
	ctx := t.Context()
	roster := openRoster(t, 8, 4)
	caller := seniorSeed(t, "caller", "", 0)

	roster.ObserveCaller(ctx, caller, yagomodel.PeerJunior)

	if _, found := roster.PeerByHash(ctx, caller.Hash); found {
		t.Fatal("addressless caller entered the roster")
	}
	if roster.KnownPeerCount(ctx) != 0 || roster.ReachablePeerCount(ctx) != 0 {
		t.Fatalf(
			"addressless caller counts = %d/%d, want 0/0",
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}
}

func TestCallerObservationKeepsReservoirBounded(t *testing.T) {
	ctx := t.Context()
	roster := openRoster(t, 2, 1)
	first := seniorSeed(t, "first", "203.0.113.1", 8090)
	second := seniorSeed(t, "second", "203.0.113.2", 8090)
	third := seniorSeed(t, "third", "203.0.113.3", 8090)

	roster.ObserveCaller(ctx, first, yagomodel.PeerJunior)
	roster.ObserveCaller(ctx, second, yagomodel.PeerJunior)
	roster.ObserveCaller(ctx, third, yagomodel.PeerJunior)

	if known := roster.KnownPeerCount(ctx); known != 2 {
		t.Fatalf("known callers = %d, want reservoir bound 2", known)
	}
	if _, found := roster.PeerByHash(ctx, first.Hash); found {
		t.Fatal("stalest junior caller was not evicted")
	}
}

func TestCallerObservationRejectsUnclassifiedPeer(t *testing.T) {
	roster := openRoster(t, 8, 4)
	caller := seniorSeed(t, "caller", "203.0.113.2", 8090)

	roster.ObserveCaller(t.Context(), caller, yagomodel.PeerVirgin)

	if known := roster.KnownPeerCount(t.Context()); known != 0 {
		t.Fatalf("known callers = %d, want invalid observation ignored", known)
	}
}

func TestCallerObservationConcurrentRosterRemainsBounded(t *testing.T) {
	ctx := t.Context()
	roster := openConcurrentRoster(t, 16, 4)
	var writers sync.WaitGroup
	for index := range 64 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			caller := seniorSeed(
				t,
				fmt.Sprintf("%012d", index),
				"203.0.113.2",
				8090,
			)
			classification := yagomodel.PeerJunior
			if index%3 == 0 {
				classification = yagomodel.PeerSenior
			}
			roster.ObserveCaller(ctx, caller, classification)
		}()
	}
	writers.Wait()
	shared := seniorSeed(t, "shared", "203.0.113.3", 8090)
	for index := range 32 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			classification := yagomodel.PeerJunior
			if index%2 == 0 {
				classification = yagomodel.PeerSenior
			}
			roster.ObserveCaller(ctx, shared, classification)
		}()
	}
	writers.Wait()
	roster.ObserveCaller(ctx, shared, yagomodel.PeerJunior)

	if known := roster.KnownPeerCount(ctx); known > 16 {
		t.Fatalf("known callers = %d, maximum 16", known)
	}
	if reachable := roster.ReachablePeers(ctx); len(reachable) > 4 {
		t.Fatalf("reachable callers = %d, maximum 4", len(reachable))
	} else {
		for _, peer := range reachable {
			classification, _ := peer.PeerType.Get()
			if classification == yagomodel.PeerJunior {
				t.Fatalf("junior caller entered reachable set: %#v", peer)
			}
		}
	}
	stored, found := roster.PeerByHash(ctx, shared.Hash)
	classification, classified := stored.PeerType.Get()
	if !found || !classified || classification != yagomodel.PeerSenior {
		t.Fatalf("final shared caller = %#v/%v", stored, found)
	}
}

func TestPeerByHashResolvesDiscoveredPeer(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	roster.Discover(ctx, senior)

	got, ok := roster.PeerByHash(ctx, senior.Hash)
	if !ok {
		t.Fatal("a discovered peer must resolve by hash")
	}
	if got.Hash != senior.Hash {
		t.Fatalf("hash = %q, want %q", got.Hash, senior.Hash)
	}

	ghost := seniorSeed(t, "ghost", "203.0.113.9", 9099)
	if _, ok := roster.PeerByHash(ctx, ghost.Hash); ok {
		t.Fatal("an undiscovered hash must not resolve")
	}
}

func TestReachablePromotesAndIsServed(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	roster.Discover(ctx, senior)

	if got := roster.ReachablePeers(ctx); len(got) != 0 {
		t.Fatalf("reachable before greet = %d, want 0", len(got))
	}

	roster.ConfirmReachable(ctx, senior.Hash)

	if _, ok := hashes(roster.ReachablePeers(ctx))[senior.Hash]; !ok {
		t.Fatalf("senior not served as reachable after confirmation")
	}
}

func TestPeerCountsFollowRosterState(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	first := seniorSeed(t, "first", "203.0.113.1", 8090)
	second := seniorSeed(t, "second", "203.0.113.2", 8090)
	roster.Discover(ctx, first, second)
	if roster.KnownPeerCount(ctx) != 2 || roster.ReachablePeerCount(ctx) != 0 {
		t.Fatalf(
			"counts after discovery = %d/%d, want 2/0",
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}

	roster.ConfirmReachable(ctx, first.Hash)
	if roster.KnownPeerCount(ctx) != 2 || roster.ReachablePeerCount(ctx) != 1 {
		t.Fatalf(
			"counts after reachability = %d/%d, want 2/1",
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}

	roster.ConfirmUnreachable(ctx, first.Hash)
	if roster.KnownPeerCount(ctx) != 2 || roster.ReachablePeerCount(ctx) != 0 {
		t.Fatalf(
			"counts after unreachable = %d/%d, want 2/0",
			roster.KnownPeerCount(ctx),
			roster.ReachablePeerCount(ctx),
		)
	}
}

func TestReachableUnknownPeerIsNoop(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	roster.ConfirmReachable(ctx, hashFor("ghost"))

	if got := roster.ReachablePeers(ctx); len(got) != 0 {
		t.Fatalf("reachable = %d, want 0 for unknown peer", len(got))
	}
}

func TestUnreachableDropsFromReachableAndReservoir(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	roster.Discover(ctx, senior)
	roster.ConfirmReachable(ctx, senior.Hash)

	roster.ConfirmUnreachable(ctx, senior.Hash)

	if got := roster.ReachablePeers(ctx); len(got) != 0 {
		t.Fatalf("reachable = %d, want 0 after failure", len(got))
	}
	if got := roster.FreshestPeers(ctx, 4); len(got) != 0 {
		t.Fatalf("greet targets = %d, want 0 after drop", len(got))
	}
}

func TestRejectRemoteIndexClearsFlagForClashingAddress(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)
	peer := seniorSeed(t, "senior", "203.0.113.1", 8090)
	peer.Flags = yagomodel.Some(
		yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
	)
	roster.Discover(ctx, peer)
	roster.ConfirmReachable(ctx, peer.Hash)

	roster.RejectRemoteIndex(ctx, seniorSeed(t, "senior", "203.0.113.1", 8091))

	reachable := roster.ReachablePeers(ctx)
	if len(reachable) != 1 || reachable[0].Hash != peer.Hash {
		t.Fatalf("reachable = %#v, want peer retained", reachable)
	}
	flags, ok := reachable[0].Flags.Get()
	if !ok || flags.Get(yagomodel.FlagAcceptRemoteIndex) {
		t.Fatalf("flags = %q, %v; want remote-index disabled", flags, ok)
	}
}

func TestRejectRemoteIndexKeepsFlagForDifferentCurrentAddress(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)
	peer := seniorSeed(t, "senior", "203.0.113.2", 8090)
	peer.Flags = yagomodel.Some(
		yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
	)
	roster.Discover(ctx, peer)
	roster.ConfirmReachable(ctx, peer.Hash)

	roster.RejectRemoteIndex(ctx, seniorSeed(t, "senior", "203.0.113.1", 8090))

	reachable := roster.ReachablePeers(ctx)
	flags, ok := reachable[0].Flags.Get()
	if !ok || !flags.Get(yagomodel.FlagAcceptRemoteIndex) {
		t.Fatalf("flags = %q, %v; want remote-index retained", flags, ok)
	}
}

func TestRejectRemoteIndexSetsMissingFlagsToDisabled(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)
	peer := seniorSeed(t, "senior", "203.0.113.1", 8090)
	roster.Discover(ctx, peer)
	roster.ConfirmReachable(ctx, peer.Hash)

	roster.RejectRemoteIndex(ctx, peer)

	flags, ok := roster.ReachablePeers(ctx)[0].Flags.Get()
	if !ok || flags.Get(yagomodel.FlagAcceptRemoteIndex) {
		t.Fatalf("flags = %q, %v; want explicit remote-index disabled", flags, ok)
	}
}

func TestDiscoverEvictsStalestBeyondCapacity(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 2, 4)

	oldest := seniorSeed(t, "oldest", "203.0.113.1", 8090)
	middle := seniorSeed(t, "middle", "203.0.113.2", 8090)
	newest := seniorSeed(t, "newest", "203.0.113.3", 8090)

	roster.Discover(ctx, oldest)
	roster.Discover(ctx, middle)
	roster.Discover(ctx, newest)

	targets := hashes(roster.FreshestPeers(ctx, 4))
	if _, ok := targets[oldest.Hash]; ok {
		t.Fatalf("stalest peer should have been evicted: %v", targets)
	}
	if len(targets) != 2 {
		t.Fatalf("reservoir size = %d, want 2 after eviction", len(targets))
	}
}

func TestFreshestPeersToppedUpToLimit(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 2)

	for _, name := range []string{"a", "b", "c", "d"} {
		roster.Discover(ctx, seniorSeed(t, name, "203.0.113.9", 8090+len(name)+int(name[0])))
	}

	if got := len(roster.FreshestPeers(ctx, 2)); got != 2 {
		t.Fatalf("freshest peers = %d, want capped at limit 2", got)
	}
}
