package peerroster

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type PeerObservation struct {
	Seed     yagomodel.Seed
	LastSeen time.Time
}

type ObservationReader interface {
	PeerObservations(ctx context.Context) ([]PeerObservation, int, int, error)
	PeerObservation(
		ctx context.Context,
		peer yagomodel.Hash,
	) (PeerObservation, bool, error)
}

func (r *roster) ObservedKnownPeerCount(ctx context.Context) (int, error) {
	count := 0
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		observed, err := r.peers.Len(tx)
		if err != nil {
			return fmt.Errorf("count observed peers: %w", err)
		}
		count = observed

		return nil
	}); err != nil {
		return 0, fmt.Errorf("count observed peers: %w", err)
	}

	return max(0, count), nil
}

func (r *roster) PeerObservations(
	ctx context.Context,
) ([]PeerObservation, int, int, error) {
	observations := make([]PeerObservation, 0, max(r.reservoirCap, 0))
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.peers.Scan(tx, nil, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("read peer observations: %w", err)
			}
			observations = append(observations, PeerObservation{
				Seed:     detachCandidateSeed(entry.seed),
				LastSeen: entry.lastSeen,
			})

			return true, nil
		})
	}); err != nil {
		return nil, 0, 0, fmt.Errorf("read peer observations: %w", err)
	}

	sort.SliceStable(observations, func(left, right int) bool {
		comparison := observations[left].LastSeen.Compare(observations[right].LastSeen)
		if comparison != 0 {
			return comparison > 0
		}

		return observations[left].Seed.Hash.String() < observations[right].Seed.Hash.String()
	})
	_, reachable := r.activeSnapshot()

	return observations, len(observations), len(reachable), nil
}

func (r *roster) PeerObservation(
	ctx context.Context,
	peer yagomodel.Hash,
) (PeerObservation, bool, error) {
	var (
		observation PeerObservation
		found       bool
	)
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.peers.Get(tx, r.key(peer))
		if err != nil {
			return fmt.Errorf("read peer observation: %w", err)
		}
		if known {
			observation = PeerObservation{
				Seed:     detachCandidateSeed(entry.seed),
				LastSeen: entry.lastSeen,
			}
			found = true
		}

		return nil
	}); err != nil {
		return PeerObservation{}, false, fmt.Errorf("read peer observation: %w", err)
	}

	return observation, found, nil
}
