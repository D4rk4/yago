package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

type fakePeerBlocks struct {
	blocked        map[yagomodel.Hash]struct{}
	blockErr       error
	unblockErr     error
	isBlockedErr   error
	blockedErr     error
	blockedCalls   []yagomodel.Hash
	unblockedCalls []yagomodel.Hash
}

func newFakePeerBlocks(hashes ...yagomodel.Hash) *fakePeerBlocks {
	blocked := make(map[yagomodel.Hash]struct{}, len(hashes))
	for _, hash := range hashes {
		blocked[hash] = struct{}{}
	}

	return &fakePeerBlocks{blocked: blocked}
}

func (f *fakePeerBlocks) Block(_ context.Context, hash yagomodel.Hash) error {
	f.blockedCalls = append(f.blockedCalls, hash)
	if f.blockErr != nil {
		return f.blockErr
	}
	f.blocked[hash] = struct{}{}

	return nil
}

func (f *fakePeerBlocks) Unblock(_ context.Context, hash yagomodel.Hash) error {
	f.unblockedCalls = append(f.unblockedCalls, hash)
	if f.unblockErr != nil {
		return f.unblockErr
	}
	delete(f.blocked, hash)

	return nil
}

func (f *fakePeerBlocks) IsBlocked(_ context.Context, hash yagomodel.Hash) (bool, error) {
	if f.isBlockedErr != nil {
		return false, f.isBlockedErr
	}
	_, ok := f.blocked[hash]

	return ok, nil
}

func (f *fakePeerBlocks) Blocked(context.Context) ([]peerblock.Blocked, error) {
	if f.blockedErr != nil {
		return nil, f.blockedErr
	}
	blocked := make([]peerblock.Blocked, 0, len(f.blocked))
	for hash := range f.blocked {
		blocked = append(blocked, peerblock.Blocked{Hash: hash})
	}

	return blocked, nil
}

func TestBlockingRosterExcludesBlockedFromReachable(t *testing.T) {
	ctx := context.Background()
	blocked := yagomodel.Hash("AAAAAAAAAAAA")
	allowed := yagomodel.Hash("BBBBBBBBBBBB")
	inner := reachableRoster{peers: []yagomodel.Seed{{Hash: blocked}, {Hash: allowed}}}
	roster := newBlockingRoster(inner, newFakePeerBlocks(blocked))

	peers := roster.ReachablePeers(ctx)
	if len(peers) != 1 || peers[0].Hash != allowed {
		t.Fatalf("reachable = %+v, want only the allowed peer", peers)
	}
	if roster.ReachablePeerCount(ctx) != 1 {
		t.Fatalf("reachable count = %d, want 1", roster.ReachablePeerCount(ctx))
	}
	if got := roster.FreshestPeers(ctx, 10); len(got) != 2 {
		t.Fatalf("FreshestPeers should be delegated unfiltered, got %d", len(got))
	}
}

func TestHelloPeerRosterSelectsFreshestUnblockedPeers(t *testing.T) {
	ctx := t.Context()
	blocked := yagomodel.Hash("AAAAAAAAAAAA")
	first := yagomodel.Hash("BBBBBBBBBBBB")
	second := yagomodel.Hash("CCCCCCCCCCCC")
	inner := reachableRoster{peers: []yagomodel.Seed{
		{Hash: blocked},
		{Hash: first},
		{Hash: second},
	}}
	roster := newBlockingRoster(inner, newFakePeerBlocks(blocked))

	peers := (helloPeerRoster{roster: roster}).FreshestPeers(ctx, 2)
	if len(peers) != 2 || peers[0].Hash != first || peers[1].Hash != second {
		t.Fatalf("hello peers = %+v, want freshest unblocked peers", peers)
	}
}

func TestBlockingRosterPassesThroughWithoutBlocks(t *testing.T) {
	ctx := context.Background()
	inner := reachableRoster{peers: []yagomodel.Seed{
		{Hash: yagomodel.Hash("AAAAAAAAAAAA")},
		{Hash: yagomodel.Hash("BBBBBBBBBBBB")},
	}}
	roster := newBlockingRoster(inner, newFakePeerBlocks())

	if len(roster.ReachablePeers(ctx)) != 2 {
		t.Fatal("an empty blocklist must not drop any reachable peer")
	}
}

func TestBlockingRosterFailsOpenOnReadError(t *testing.T) {
	ctx := context.Background()
	inner := reachableRoster{peers: []yagomodel.Seed{
		{Hash: yagomodel.Hash("AAAAAAAAAAAA")},
		{Hash: yagomodel.Hash("BBBBBBBBBBBB")},
	}}
	blocks := newFakePeerBlocks()
	blocks.blockedErr = errors.New("read failed")
	roster := newBlockingRoster(inner, blocks)

	if len(roster.ReachablePeers(ctx)) != 2 {
		t.Fatal("a blocklist read error must fail open, not drop every peer")
	}
}

func TestBlockingRosterForwardsPeerObservations(t *testing.T) {
	when := time.Unix(100, 0)
	base := &observationCountingRoster{observation: peerroster.PeerObservation{
		Seed: yagomodel.Seed{Hash: yagomodel.Hash("AAAAAAAAAAAA")}, LastSeen: when,
	}}
	reader := newBlockingRoster(base, newFakePeerBlocks()).(peerroster.ObservationReader)

	observations, known, reachable, err := reader.PeerObservations(t.Context())
	if err != nil || known != 1 || reachable != 0 || len(observations) != 1 ||
		observations[0].LastSeen != when {
		t.Fatalf("PeerObservations = %+v/%d/%d/%v", observations, known, reachable, err)
	}
	observation, found, err := reader.PeerObservation(
		t.Context(), yagomodel.Hash("AAAAAAAAAAAA"),
	)
	if err != nil || !found || observation.LastSeen != when {
		t.Fatalf("PeerObservation = %+v/%v/%v", observation, found, err)
	}

	base.err = errors.New("read failed")
	if _, _, _, err := reader.PeerObservations(t.Context()); err == nil {
		t.Fatal("peer observation scan error was discarded")
	}
	if _, _, err := reader.PeerObservation(
		t.Context(), yagomodel.Hash("AAAAAAAAAAAA"),
	); err == nil {
		t.Fatal("peer observation read error was discarded")
	}

	missing := newBlockingRoster(reachableRoster{}, newFakePeerBlocks()).(peerroster.ObservationReader)
	if _, _, _, err := missing.PeerObservations(t.Context()); !errors.Is(
		err,
		errPeerObservationsUnavailable,
	) {
		t.Fatalf("missing PeerObservations error = %v", err)
	}
	if _, _, err := missing.PeerObservation(
		t.Context(), yagomodel.Hash("AAAAAAAAAAAA"),
	); !errors.Is(err, errPeerObservationsUnavailable) {
		t.Fatalf("missing PeerObservation error = %v", err)
	}
}

func TestPeerBlockControllerBlocksValidPeer(t *testing.T) {
	blocks := newFakePeerBlocks()
	ctrl := newPeerBlockController(blocks, yagomodel.Hash("SSSSSSSSSSSS"))

	if err := ctrl.Block(context.Background(), "AAAAAAAAAAAA"); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if len(blocks.blockedCalls) != 1 || blocks.blockedCalls[0] != yagomodel.Hash("AAAAAAAAAAAA") {
		t.Fatalf("block calls = %v", blocks.blockedCalls)
	}
}

func TestPeerBlockControllerRejectsSelf(t *testing.T) {
	self := yagomodel.Hash("BBBBBBBBBBBB")
	ctrl := newPeerBlockController(newFakePeerBlocks(), self)

	if err := ctrl.Block(
		context.Background(),
		"BBBBBBBBBBBB",
	); !errors.Is(
		err,
		errCannotBlockSelf,
	) {
		t.Fatalf("err = %v, want errCannotBlockSelf", err)
	}
}

func TestPeerBlockControllerRejectsInvalidHash(t *testing.T) {
	ctrl := newPeerBlockController(newFakePeerBlocks(), yagomodel.Hash("SSSSSSSSSSSS"))

	if err := ctrl.Block(context.Background(), "too-short"); err == nil {
		t.Fatal("Block should reject a malformed hash")
	}
	if err := ctrl.Unblock(context.Background(), "too-short"); err == nil {
		t.Fatal("Unblock should reject a malformed hash")
	}
}

func TestPeerBlockControllerUnblocksValidPeer(t *testing.T) {
	blocks := newFakePeerBlocks(yagomodel.Hash("AAAAAAAAAAAA"))
	ctrl := newPeerBlockController(blocks, yagomodel.Hash("SSSSSSSSSSSS"))

	if err := ctrl.Unblock(context.Background(), "AAAAAAAAAAAA"); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if len(blocks.unblockedCalls) != 1 {
		t.Fatalf("unblock calls = %v", blocks.unblockedCalls)
	}
}

func TestPeerBlockControllerSurfacesStoreErrors(t *testing.T) {
	blockFail := newFakePeerBlocks()
	blockFail.blockErr = errors.New("disk full")
	if err := newPeerBlockController(blockFail, yagomodel.Hash("SSSSSSSSSSSS")).
		Block(context.Background(), "AAAAAAAAAAAA"); err == nil {
		t.Fatal("Block should surface a store write failure")
	}

	unblockFail := newFakePeerBlocks(yagomodel.Hash("AAAAAAAAAAAA"))
	unblockFail.unblockErr = errors.New("disk full")
	if err := newPeerBlockController(unblockFail, yagomodel.Hash("SSSSSSSSSSSS")).
		Unblock(context.Background(), "AAAAAAAAAAAA"); err == nil {
		t.Fatal("Unblock should surface a store write failure")
	}
}

func TestPeerDetailSourceMarksBlocked(t *testing.T) {
	seed := networkTestSeed(t)
	blocks := newFakePeerBlocks(seed.Hash)
	source := newPeerDetailSource(reachableRoster{peers: []yagomodel.Seed{seed}}, blocks)

	detail, ok, err := source.PeerDetail(context.Background(), string(seed.Hash))
	if err != nil || !ok || !detail.BlockStatusKnown || !detail.Blocked {
		t.Fatalf("detail.Blocked = %v (ok=%v), want blocked", detail.Blocked, ok)
	}
}

func TestPeerDetailSourceMarksBlockStatusUnknownOnLookupError(t *testing.T) {
	seed := networkTestSeed(t)
	blocks := newFakePeerBlocks()
	blocks.isBlockedErr = errors.New("read failed")
	source := newPeerDetailSource(reachableRoster{peers: []yagomodel.Seed{seed}}, blocks)

	detail, ok, err := source.PeerDetail(context.Background(), string(seed.Hash))
	if err != nil || !ok || detail.BlockStatusKnown {
		t.Fatal("a block lookup error should mark block status unknown")
	}
}

func TestNetworkSourceMarksBlockedPeers(t *testing.T) {
	seed := networkTestSeed(t)
	blocks := newFakePeerBlocks(seed.Hash)
	source := newNetworkSource(
		dhtGateStatusSource{}, reachableRoster{peers: []yagomodel.Seed{seed}}, nil, nil, blocks,
	)

	status := source.Network(context.Background())
	if len(status.Peers) != 1 || !status.Peers[0].BlockStatusKnown || !status.Peers[0].Blocked {
		t.Fatalf("peers = %+v, want the peer marked blocked", status.Peers)
	}
}

func TestNetworkSourceMarksPeerBlockStatusUnknownOnReadError(t *testing.T) {
	seed := networkTestSeed(t)
	blocks := newFakePeerBlocks()
	blocks.blockedErr = errors.New("read failed")
	source := newNetworkSource(
		dhtGateStatusSource{}, reachableRoster{peers: []yagomodel.Seed{seed}}, nil, nil, blocks,
	)

	status := source.Network(context.Background())
	if len(status.Peers) != 1 || status.Peers[0].BlockStatusKnown {
		t.Fatal("a blocklist read error should mark table block status unknown")
	}
}
