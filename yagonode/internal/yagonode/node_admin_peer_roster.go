package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

const adminPeerRosterReadFailedMessage = "read peer roster for admin failed"

type adminPeerRosterSnapshot struct {
	seeds          []yagomodel.Seed
	knownPeers     int
	reachablePeers int
	available      bool
}

func (s networkSource) peerRosterSnapshot(ctx context.Context) adminPeerRosterSnapshot {
	reader, observed := s.roster.(peerroster.ObservationReader)
	if !observed {
		knownPeers := s.roster.KnownPeerCount(ctx)

		return adminPeerRosterSnapshot{
			seeds:          s.roster.FreshestPeers(ctx, knownPeers),
			knownPeers:     knownPeers,
			reachablePeers: s.roster.ReachablePeerCount(ctx),
			available:      true,
		}
	}

	observations, knownPeers, reachablePeers, err := reader.PeerObservations(ctx)
	if err != nil {
		slog.WarnContext(ctx, adminPeerRosterReadFailedMessage, slog.Any("error", err))

		return adminPeerRosterSnapshot{}
	}

	seeds := make([]yagomodel.Seed, 0, len(observations))
	for _, observation := range observations {
		seeds = append(seeds, seedWithLocalObservation(observation.Seed, observation.LastSeen))
	}

	return adminPeerRosterSnapshot{
		seeds:          seeds,
		knownPeers:     knownPeers,
		reachablePeers: reachablePeers,
		available:      true,
	}
}

func readAdminPeer(
	ctx context.Context,
	roster peerroster.Roster,
	hash yagomodel.Hash,
) (yagomodel.Seed, bool, error) {
	reader, observed := roster.(peerroster.ObservationReader)
	if !observed {
		seed, found := roster.PeerByHash(ctx, hash)

		return seed, found, nil
	}

	observation, found, err := reader.PeerObservation(ctx, hash)
	if err != nil {
		return yagomodel.Seed{}, false, fmt.Errorf("read peer observation: %w", err)
	}
	if !found {
		return yagomodel.Seed{}, false, nil
	}

	return seedWithLocalObservation(observation.Seed, observation.LastSeen), true, nil
}

func parseAdminPeerHash(hash string) (yagomodel.Hash, bool) {
	parsed, err := yagomodel.ParseHash(hash)

	return parsed, err == nil
}

func seedWithLocalObservation(seed yagomodel.Seed, observed time.Time) yagomodel.Seed {
	seed.LastSeen = yagomodel.None[yagomodel.SeedLastSeenUTC]()
	if !observed.IsZero() {
		seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(observed))
	}

	return seed
}
