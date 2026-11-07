package peerroster

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const callerObservationRefreshInterval = 5 * time.Minute

type previousCallerObservation struct {
	known    bool
	repeated bool
}

func (r *roster) previousCallerObservation(
	ctx context.Context,
	observation rosterEntry,
	answered yagomodel.Optional[peerwire.Endpoint],
) (previousCallerObservation, error) {
	var previous previousCallerObservation
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		key := r.key(observation.seed.Hash)
		stored, _, _ := r.getRosterEntry(tx, key)
		_, answeredChanged := r.answeredEndpointChange(tx, key, observation, answered)
		previous = previousCallerObservation{
			known:    r.peers.Contains(tx, key),
			repeated: !answeredChanged && callerObservationRepeats(stored, observation),
		}

		return nil
	}); err != nil {
		return previousCallerObservation{}, fmt.Errorf("read previous caller observation: %w", err)
	}

	return previous, nil
}

func callerObservationRepeats(stored, observation rosterEntry) bool {
	elapsed := observation.lastSeen.Sub(stored.lastSeen)

	return elapsed >= 0 && elapsed < callerObservationRefreshInterval &&
		stored.verified == observation.verified &&
		stored.retryAfter.IsZero() &&
		seedWithoutStatistics(
			stored.seed,
		).String() ==
			seedWithoutStatistics(
				observation.seed,
			).String()
}

func seedWithoutStatistics(seed yagomodel.Seed) yagomodel.Seed {
	stable := seed.Copy()
	stable.Uptime = yagomodel.None[int]()
	stable.LastSeen = yagomodel.None[yagomodel.SeedLastSeenUTC]()
	stable.RWICount = yagomodel.None[int]()
	stable.URLCount = yagomodel.None[int]()
	stable.NoticedURLCount = yagomodel.None[int]()
	stable.OfferedURLCount = yagomodel.None[int]()
	stable.KnownSeedCount = yagomodel.None[int]()
	stable.ConnectsPerHour = yagomodel.None[int]()
	stable.IndexingSpeed = yagomodel.None[int]()
	stable.RequestSpeed = yagomodel.None[int]()
	stable.UplinkSpeed = yagomodel.None[int]()
	stable.SentWordCount = yagomodel.None[int64]()
	stable.ReceivedWordCount = yagomodel.None[int64]()
	stable.SentURLCount = yagomodel.None[int64]()
	stable.ReceivedURLCount = yagomodel.None[int64]()

	return stable
}
