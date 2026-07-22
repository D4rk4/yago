package peerroster

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestCandidateSnapshotCachesAndInvalidatesRosterMutations(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	var scans atomic.Int64
	engine.scanObserver = func(name vault.Name) {
		if name == peersBucket {
			scans.Add(1)
		}
	}
	first := internalSeed(t, "first", "203.0.113.1")
	first.Flags = yagomodel.Some(
		yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
	)
	second := internalSeed(t, "second", "203.0.113.2")
	r.Discover(t.Context(), first, second)

	if got := len(r.FreshestPeers(t.Context(), 8)); got != 2 {
		t.Fatalf("initial candidates = %d, want 2", got)
	}
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 2 || scans.Load() != 1 {
		t.Fatalf("cached candidates/scans = %d/%d, want 2/1", got, scans.Load())
	}
	r.Discover(t.Context(), first)
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 2 || scans.Load() != 1 {
		t.Fatalf("known discovery candidates/scans = %d/%d, want 2/1", got, scans.Load())
	}

	third := internalSeed(t, "third", "203.0.113.3")
	r.Discover(t.Context(), third)
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 3 || scans.Load() != 2 {
		t.Fatalf("discovery candidates/scans = %d/%d, want 3/2", got, scans.Load())
	}
	r.ConfirmReachable(t.Context(), first.Hash)
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 3 || scans.Load() != 3 {
		t.Fatalf("reachable candidates/scans = %d/%d, want 3/3", got, scans.Load())
	}
	r.RejectRemoteIndex(t.Context(), first)
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 3 || scans.Load() != 4 {
		t.Fatalf("rejected candidates/scans = %d/%d, want 3/4", got, scans.Load())
	}
	r.ConfirmUnreachable(t.Context(), first.Hash)
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 2 || scans.Load() != 5 {
		t.Fatalf("unreachable candidates/scans = %d/%d, want 2/5", got, scans.Load())
	}

	revision := r.candidateRevision
	r.ConfirmUnreachable(t.Context(), internalHashFor("unknown"))
	r.RejectRemoteIndex(t.Context(), internalSeed(t, "second", "203.0.113.99"))
	if got := len(r.FreshestPeers(t.Context(), 8)); got != 2 || scans.Load() != 5 {
		t.Fatalf("noop candidates/scans = %d/%d, want 2/5", got, scans.Load())
	}
	if r.candidateRevision != revision {
		t.Fatalf("noop mutations changed revision from %d to %d", revision, r.candidateRevision)
	}
}

func TestCandidateSnapshotCoalescesConcurrentBuilds(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var scans atomic.Int64
	engine.scanObserver = func(name vault.Name) {
		if name != peersBucket {
			return
		}
		scans.Add(1)
		once.Do(func() {
			close(entered)
			<-release
		})
	}

	results := make(chan []yagomodel.Seed, 2)
	go func() {
		results <- r.FreshestPeers(t.Context(), 8)
	}()
	<-entered
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		results <- r.FreshestPeers(t.Context(), 8)
	}()
	<-secondStarted
	time.Sleep(10 * time.Millisecond)
	close(release)

	for range 2 {
		if got := len(<-results); got != 1 {
			t.Fatalf("candidate result = %d, want 1", got)
		}
	}
	if scans.Load() != 1 {
		t.Fatalf("coalesced scans = %d, want 1", scans.Load())
	}
}

func TestCandidateSnapshotWaitHonorsContext(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	building := make(chan struct{})
	r.candidateMu.Lock()
	r.candidateBuilding = building
	r.candidateMu.Unlock()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if got := r.FreshestPeers(ctx, 8); got != nil {
		t.Fatalf("canceled wait candidates = %#v, want nil", got)
	}

	r.candidateMu.Lock()
	r.candidateBuilding = nil
	close(building)
	r.candidateMu.Unlock()
}

func TestCandidateSnapshotRebuildsAfterConcurrentInvalidation(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	entered := make(chan struct{})
	release := make(chan struct{})
	var scans atomic.Int64
	engine.scanObserver = func(name vault.Name) {
		if name != peersBucket {
			return
		}
		if scans.Add(1) == 1 {
			close(entered)
			<-release
		}
	}

	result := make(chan []yagomodel.Seed, 1)
	go func() {
		result <- r.FreshestPeers(t.Context(), 8)
	}()
	<-entered
	r.invalidateCandidateSnapshot()
	close(release)
	if got := len(<-result); got != 1 {
		t.Fatalf("rebuilt candidates = %d, want 1", got)
	}
	if scans.Load() != 2 {
		t.Fatalf("rebuild scans = %d, want 2", scans.Load())
	}
}

func TestCandidateScansHonorContextCancellation(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	candidateContext, cancelCandidates := context.WithCancel(t.Context())
	engine.scanObserver = func(name vault.Name) {
		if name == peersBucket {
			cancelCandidates()
		}
	}
	if got := r.FreshestPeers(candidateContext, 8); got != nil {
		t.Fatalf("canceled candidate scan = %#v, want nil", got)
	}
	cancelCandidates()
	if r.candidateReady || r.candidateBuilding != nil {
		t.Fatal("canceled candidate scan was published")
	}

	inactiveContext, cancelInactive := context.WithCancel(t.Context())
	engine.scanObserver = func(name vault.Name) {
		if name == peersBucket {
			cancelInactive()
		}
	}
	if got := r.selectInactive(
		inactiveContext,
		nil,
		1,
		func(left, right rosterEntry) bool { return left.lastSeen.Before(right.lastSeen) },
	); got != nil {
		t.Fatalf("canceled inactive scan = %#v, want nil", got)
	}
	cancelInactive()

	canceled, stop := context.WithCancel(t.Context())
	stop()
	if _, _, err := r.buildCandidateSnapshot(canceled); err == nil {
		t.Fatal("canceled candidate build returned no error")
	}
	if got := r.FreshestPeers(canceled, 8); got != nil {
		t.Fatalf("pre-canceled candidates = %#v, want nil", got)
	}
}

func TestCandidateSnapshotBoundsBytesCountAndFreshness(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	if got := r.FreshestPeers(t.Context(), 0); got != nil {
		t.Fatalf("zero-limit candidates = %#v, want nil", got)
	}
	now := time.Unix(100, 0)
	r.now = func() time.Time {
		now = now.Add(time.Second)

		return now
	}
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	third := internalSeed(t, "third", "203.0.113.3")
	r.candidateByteLimit = 2 * (first.RetainedBytes() + rosterCandidateRetentionBytes)
	r.Discover(t.Context(), first)
	r.Discover(t.Context(), second)
	r.Discover(t.Context(), third)

	got := r.FreshestPeers(t.Context(), candidateSnapshotMaximumPeers+1)
	if len(got) != 2 || got[0].Hash != third.Hash || got[1].Hash != second.Hash {
		t.Fatalf("bounded candidates = %#v, want third then second", got)
	}
	if r.candidateBytes > r.candidateByteLimit {
		t.Fatalf("candidate bytes = %d, maximum %d", r.candidateBytes, r.candidateByteLimit)
	}
	got[0].Hash = internalHashFor("changed")
	if cached := r.FreshestPeers(t.Context(), 1); len(cached) != 1 || cached[0].Hash != third.Hash {
		t.Fatalf("cached candidate mutated through caller: %#v", cached)
	}

	r.candidateByteLimit = first.RetainedBytes() + rosterCandidateRetentionBytes - 1
	r.invalidateCandidateSnapshot()
	if oversized := r.FreshestPeers(t.Context(), 8); len(oversized) != 0 {
		t.Fatalf("oversized candidates = %d, want 0", len(oversized))
	}
	r.candidateByteLimit = 0
	r.invalidateCandidateSnapshot()
	if zeroBytes := r.FreshestPeers(t.Context(), 8); len(zeroBytes) != 0 {
		t.Fatalf("zero-byte candidates = %d, want 0", len(zeroBytes))
	}
	r.candidateByteLimit = candidateSnapshotMaximumBytes
	r.reservoirCap = 0
	r.invalidateCandidateSnapshot()
	if zeroPeers := r.FreshestPeers(t.Context(), 8); len(zeroPeers) != 0 {
		t.Fatalf("zero-peer candidates = %d, want 0", len(zeroPeers))
	}
	if entries, retained, err := r.scanFreshestCandidates(
		t.Context(),
		nil,
		nil,
		0,
		1,
	); err != nil || entries != nil ||
		retained != 0 {
		t.Fatalf("zero-limit scan = %#v, %d, %v", entries, retained, err)
	}
}

func TestCandidateSnapshotBoundsActiveSeeds(t *testing.T) {
	r, _ := openScriptedRoster(t, 1, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.ObserveResponder(t.Context(), first)
	r.ObserveResponder(t.Context(), second)

	seeds, retained, err := r.buildCandidateSnapshot(t.Context())
	if err != nil {
		t.Fatalf("buildCandidateSnapshot: %v", err)
	}
	if len(seeds) != 1 || retained > candidateSnapshotMaximumBytes {
		t.Fatalf("active candidates/bytes = %d/%d, want one bounded seed", len(seeds), retained)
	}

	r.candidateByteLimit = first.RetainedBytes() - 1
	seeds, retained, err = r.buildCandidateSnapshot(t.Context())
	if err != nil {
		t.Fatalf("buildCandidateSnapshot oversized active: %v", err)
	}
	if len(seeds) != 0 || retained != 0 {
		t.Fatalf("oversized active candidates/bytes = %d/%d, want 0/0", len(seeds), retained)
	}
}

func TestCandidateAndReachableSnapshotsDetachIP6(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	hosts, err := yagomodel.ParseIP6("2001:db8::1")
	if err != nil {
		t.Fatalf("ParseIP6: %v", err)
	}
	peer.IP6 = yagomodel.Some(hosts)
	r.Discover(t.Context(), peer)

	candidates := r.FreshestPeers(t.Context(), 8)
	candidateHosts, _ := candidates[0].IP6.Get()
	candidateHosts[0] = parseCandidateHost(t, "2001:db8::2")
	candidates[0].Hash = internalHashFor("changed")
	again := r.FreshestPeers(t.Context(), 8)
	againHosts, _ := again[0].IP6.Get()
	if again[0].Hash != peer.Hash || againHosts[0].String() != "2001:db8::1" {
		t.Fatalf("candidate snapshot retained caller mutation: %#v", again)
	}

	r.ConfirmReachable(t.Context(), peer.Hash)
	reachable := r.ReachablePeers(t.Context())
	reachableHosts, _ := reachable[0].IP6.Get()
	reachableHosts[0] = parseCandidateHost(t, "2001:db8::3")
	reachableAgain := r.ReachablePeers(t.Context())
	reachableAgainHosts, _ := reachableAgain[0].IP6.Get()
	if reachableAgainHosts[0].String() != "2001:db8::1" {
		t.Fatalf("reachable snapshot retained caller mutation: %#v", reachableAgain)
	}
}

func TestCandidateSnapshotRetainsCompactStrings(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	backing := strings.Repeat("x", 1<<20) + "peer-name" + strings.Repeat("y", 1<<20)
	name := backing[1<<20 : 1<<20+len("peer-name")]
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.Name = yagomodel.Some(name)
	r.Discover(t.Context(), peer)
	retained, _ := r.FreshestPeers(t.Context(), 8)[0].Name.Get()
	backingStart := reflect.ValueOf(backing).Pointer()
	backingEnd := backingStart + uintptr(len(backing))
	retainedStart := reflect.ValueOf(retained).Pointer()
	if retainedStart >= backingStart && retainedStart < backingEnd {
		t.Fatal("candidate retained source name backing")
	}
}

func TestDiscoverBoundsRotatingAndBatchSeeds(t *testing.T) {
	r, _ := openScriptedRoster(t, 4, 2)
	for index := range 64 {
		peer := internalSeed(t, fmt.Sprintf("%012d", index), "203.0.113.1")
		peer.Port = yagomodel.Some(yagomodel.Port(10_000 + index))
		r.Discover(t.Context(), peer)
		if known := r.KnownPeerCount(t.Context()); known > 4 {
			t.Fatalf("rotating known peers = %d, maximum 4", known)
		}
		candidates := r.FreshestPeers(t.Context(), candidateSnapshotMaximumPeers)
		if len(candidates) > 4 || r.candidateBytes > candidateSnapshotMaximumBytes {
			t.Fatalf("rotating candidates/bytes = %d/%d", len(candidates), r.candidateBytes)
		}
	}

	batchRoster, _ := openScriptedRoster(t, peerDiscoveryMaximumSeeds+1, 2)
	batch := make([]yagomodel.Seed, peerDiscoveryMaximumSeeds+1)
	for index := range batch {
		batch[index] = internalSeed(t, fmt.Sprintf("%012d", index), "203.0.113.1")
		batch[index].Port = yagomodel.Some(yagomodel.Port(10_000 + index))
	}
	batchRoster.Discover(t.Context(), batch...)
	if known := batchRoster.KnownPeerCount(t.Context()); known != peerDiscoveryMaximumSeeds {
		t.Fatalf("batch known peers = %d, want %d", known, peerDiscoveryMaximumSeeds)
	}
	if _, found := batchRoster.PeerByHash(t.Context(), batch[len(batch)-1].Hash); found {
		t.Fatal("seed beyond discovery batch bound was retained")
	}
	if candidates := batchRoster.FreshestPeers(
		t.Context(),
		peerDiscoveryMaximumSeeds+1,
	); len(
		candidates,
	) != candidateSnapshotMaximumPeers {
		t.Fatalf("batch candidates = %d, want %d", len(candidates), candidateSnapshotMaximumPeers)
	}
}

func TestRosterEntryEncodeRejectsOversizedProgrammaticSeed(t *testing.T) {
	peer := internalSeed(t, "peer", "203.0.113.1")
	peer.Name = yagomodel.Some(strings.Repeat("n", 257))
	if _, err := (rosterEntryCodec{}).Encode(
		rosterEntry{seed: peer, lastSeen: time.Now()},
	); err == nil {
		t.Fatal("oversized roster seed encoded successfully")
	}

	r, _ := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), peer)
	if known := r.KnownPeerCount(t.Context()); known != 0 {
		t.Fatalf("oversized discovered peers = %d, want 0", known)
	}
}

func parseCandidateHost(t *testing.T, value string) yagomodel.Host {
	t.Helper()
	hosts, err := yagomodel.ParseIP6(value)
	if err != nil {
		t.Fatalf("ParseIP6(%q): %v", value, err)
	}

	return hosts[0]
}
