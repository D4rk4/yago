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

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type Roster interface {
	Discover(ctx context.Context, seeds ...yacymodel.Seed)
	ConfirmReachable(ctx context.Context, peer yacymodel.Hash)
	ConfirmUnreachable(ctx context.Context, peer yacymodel.Hash)
	FreshestPeers(ctx context.Context, limit int) []yacymodel.Seed
	ReachablePeers(ctx context.Context) []yacymodel.Seed
}

var _ Roster = (*roster)(nil)

func Open(
	storage *vault.Vault,
	now func() time.Time,
	reservoirCap int,
	activeCap int,
) (Roster, error) {
	peers, err := vault.Register(storage, peersBucket, rosterEntryCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer roster: %w", err)
	}

	return &roster{
		vault:        storage,
		peers:        peers,
		now:          now,
		reservoirCap: reservoirCap,
		activeCap:    activeCap,
		active:       make(map[yacymodel.Hash]yacymodel.Seed),
	}, nil
}
