package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

// errCannotBlockSelf prevents an operator from blocking this node's own hash,
// which would exclude the node from its own fan-out and seedlists (a lockout).
var errCannotBlockSelf = errors.New("a node cannot block itself")

// peerBlockStore is the durable blocklist the node reads for fan-out filtering
// and the console mutates.
type peerBlockStore interface {
	Block(ctx context.Context, hash yagomodel.Hash) error
	Unblock(ctx context.Context, hash yagomodel.Hash) error
	IsBlocked(ctx context.Context, hash yagomodel.Hash) (bool, error)
	Blocked(ctx context.Context) ([]peerblock.Blocked, error)
}

// blockingRoster wraps a roster so blocked peers never appear in the reachable
// set that feeds outbound index fan-out and the peer lists this node advertises
// to others. All other roster behaviour is delegated unchanged — in particular
// the admin FreshestPeers listing still shows blocked peers so they can be seen
// and unblocked.
type blockingRoster struct {
	peerroster.Roster
	blocks peerBlockStore
}

func newBlockingRoster(inner peerroster.Roster, blocks peerBlockStore) peerroster.Roster {
	return blockingRoster{Roster: inner, blocks: blocks}
}

func (r blockingRoster) ReachablePeers(ctx context.Context) []yagomodel.Seed {
	peers := r.Roster.ReachablePeers(ctx)
	if peerBlockFanoutRequestEnded(ctx) {
		return peers
	}
	blocked, err := r.blocks.Blocked(ctx)
	if err != nil {
		slog.WarnContext(ctx, peerBlockFanoutReadFailedMessage, slog.Any("error", err))

		return peers
	}
	if len(blocked) == 0 {
		return peers
	}

	excluded := make(map[yagomodel.Hash]struct{}, len(blocked))
	for _, entry := range blocked {
		excluded[entry.Hash] = struct{}{}
	}
	filtered := make([]yagomodel.Seed, 0, len(peers))
	for _, peer := range peers {
		if _, ok := excluded[peer.Hash]; ok {
			continue
		}
		filtered = append(filtered, peer)
	}

	return filtered
}

func (r blockingRoster) ReachablePeerCount(ctx context.Context) int {
	return len(r.ReachablePeers(ctx))
}

// peerBlockController adapts the durable blocklist to the console, validating the
// hash and refusing to block this node itself.
type peerBlockController struct {
	store peerBlockStore
	self  yagomodel.Hash
}

func newPeerBlockController(store peerBlockStore, self yagomodel.Hash) peerBlockController {
	return peerBlockController{store: store, self: self}
}

func (c peerBlockController) Block(ctx context.Context, hash string) error {
	parsed, err := yagomodel.ParseHash(hash)
	if err != nil {
		return fmt.Errorf("invalid peer hash: %w", err)
	}
	if parsed == c.self {
		return errCannotBlockSelf
	}
	if err := c.store.Block(ctx, parsed); err != nil {
		return fmt.Errorf("block peer: %w", err)
	}

	return nil
}

func (c peerBlockController) Unblock(ctx context.Context, hash string) error {
	parsed, err := yagomodel.ParseHash(hash)
	if err != nil {
		return fmt.Errorf("invalid peer hash: %w", err)
	}
	if err := c.store.Unblock(ctx, parsed); err != nil {
		return fmt.Errorf("unblock peer: %w", err)
	}

	return nil
}
