package recrawlfrontier

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type fetchBatchProfile struct {
	known    bool
	interval time.Duration
}

func validateFetchBatchLengths(
	urls, profileHandles []string,
	fetchedAt, sourceModifiedAt []time.Time,
) error {
	if len(urls) != len(profileHandles) || len(urls) != len(fetchedAt) {
		return fmt.Errorf("record fetches: mismatched slice lengths")
	}
	if sourceModifiedAt != nil && len(urls) != len(sourceModifiedAt) {
		return fmt.Errorf("record fetches: mismatched source-modified length")
	}

	return nil
}

func (f *Frontier) fetchBatchProfiles(
	ctx context.Context,
	profileHandles []string,
) (map[string]fetchBatchProfile, error) {
	profiles := make(map[string]fetchBatchProfile, len(profileHandles))
	for _, handle := range profileHandles {
		if _, seen := profiles[handle]; seen {
			continue
		}
		profile, found, err := f.ProfileByHandle(ctx, handle)
		if err != nil {
			return nil, fmt.Errorf("record fetches: %w", err)
		}
		profiles[handle] = fetchBatchProfile{known: found, interval: profile.RecrawlIfOlder}
	}

	return profiles, nil
}

func fetchBatchObservations(
	urls, profileHandles []string,
	fetchedAt, sourceModifiedAt []time.Time,
	profiles map[string]fetchBatchProfile,
) []fetchObservation {
	observations := make([]fetchObservation, 0, len(urls))
	for index, rawURL := range urls {
		profile := profiles[profileHandles[index]]
		if !profile.known {
			continue
		}
		modifiedAt := time.Time{}
		if sourceModifiedAt != nil {
			modifiedAt = sourceModifiedAt[index]
		}
		observations = append(observations, fetchObservation{
			url:              rawURL,
			profileHandle:    profileHandles[index],
			interval:         profile.interval,
			fetchedAt:        fetchedAt[index],
			sourceModifiedAt: modifiedAt,
		})
	}

	return observations
}

func (f *Frontier) recordFetchBatch(
	ctx context.Context,
	observations []fetchObservation,
) error {
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, observation := range observations {
			if err := f.observeInTx(tx, observation); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("store fetch batch: %w", err)
	}

	return nil
}
