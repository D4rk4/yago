// Package peerroster owns the set of network peers this node knows. It is the
// single owner of each peer's recency and reachable membership: the announcement
// loop maintains the roster from contact outcomes, while inbound admission samples
// and refreshes it. Only the bounded reachable set lives in memory; every known peer
// is persisted, so a restart resumes from the durable roster instead of the seed
// source.
package peerroster

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type Lifecycle interface {
	Discover(ctx context.Context, seeds ...yagomodel.Seed)
	ObserveCaller(ctx context.Context, caller yagomodel.Seed, classification yagomodel.PeerType)
	ObserveResponder(ctx context.Context, responder yagomodel.Seed)
	ConfirmReachable(ctx context.Context, peer yagomodel.Hash)
	ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash)
	RejectRemoteIndex(ctx context.Context, peer yagomodel.Seed)
}

type Inventory interface {
	FreshestPeers(ctx context.Context, limit int) []yagomodel.Seed
	ReachablePeers(ctx context.Context) []yagomodel.Seed
	PeerByHash(ctx context.Context, peer yagomodel.Hash) (yagomodel.Seed, bool)
	KnownPeerCount(ctx context.Context) int
	ReachablePeerCount(ctx context.Context) int
}

type Roster interface {
	Lifecycle
	Inventory
}

type Directory interface {
	SeedlistPeers(ctx context.Context, limit int) []yagomodel.Seed
	PeerByName(ctx context.Context, name string) (yagomodel.Seed, bool)
}

type Capacity struct {
	Reservoir int
	Active    int
}

var _ Roster = (*roster)(nil)

const (
	peerLifecyclesBucket             vault.Name = "peerroster_lifecycles"
	peerLifecycleCleanupCursorBucket vault.Name = "peerroster_lifecycle_cleanup"
)

func Open(
	ctx context.Context,
	storage *vault.Vault,
	self yagomodel.Hash,
	now func() time.Time,
	capacity Capacity,
) (Roster, error) {
	peers, err := vault.Register(storage, peersBucket, rosterEntryCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer roster: %w", err)
	}
	lifecycles, err := vault.RegisterKeyspace(
		storage,
		peerLifecyclesBucket,
		rosterLifecycleCodec{},
	)
	if err != nil {
		return nil, fmt.Errorf("register peer roster lifecycles: %w", err)
	}
	cleanupCursor, err := vault.RegisterKeyspace(
		storage,
		peerLifecycleCleanupCursorBucket,
		rosterLifecycleCleanupCursorCodec{},
	)
	if err != nil {
		return nil, fmt.Errorf("register peer roster lifecycle cleanup cursor: %w", err)
	}

	opened := &roster{
		vault:                  storage,
		peers:                  peers,
		lifecycles:             lifecycles,
		lifecycleCleanupCursor: cleanupCursor,
		self:                   self,
		now:                    now,
		reservoirCap:           capacity.Reservoir,
		activeCap:              capacity.Active,
		membershipPermit:       make(chan struct{}, 1),
		active:                 make(map[yagomodel.Hash]rosterEntry),
		endpointOwners:         make(map[string]endpointOwnership),
		candidateByteLimit:     candidateSnapshotMaximumBytes,
	}
	if err := opened.removeSelf(ctx); err != nil {
		return nil, fmt.Errorf("exclude self from peer roster: %w", err)
	}
	if err := opened.initializeRosterCapacity(ctx); err != nil {
		return nil, fmt.Errorf("trim peer roster overflow: %w", err)
	}
	if err := opened.cleanupRosterLifecycleOrphans(ctx); err != nil {
		return nil, fmt.Errorf("clean peer roster lifecycles: %w", err)
	}

	return opened, nil
}
