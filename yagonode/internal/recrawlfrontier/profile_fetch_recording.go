package recrawlfrontier

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (f *Frontier) RecordProfileFetch(
	ctx context.Context,
	url string,
	profile yagocrawlcontract.CrawlProfile,
	fetchedAt time.Time,
	sourceModifiedAt time.Time,
) error {
	if err := f.persistMissingProfiles(
		ctx,
		[]yagocrawlcontract.CrawlProfile{profile},
	); err != nil {
		return fmt.Errorf("record profile fetch: store profile: %w", err)
	}
	if err := f.observe(ctx, fetchObservation{
		url:              url,
		profileHandle:    profile.Handle,
		interval:         profile.RecrawlIfOlder,
		fetchedAt:        fetchedAt,
		sourceModifiedAt: sourceModifiedAt,
	}); err != nil {
		return fmt.Errorf("record profile fetch: %w", err)
	}

	return nil
}

func (f *Frontier) RecordProfileFetches(
	ctx context.Context,
	urls []string,
	profiles []yagocrawlcontract.CrawlProfile,
	fetchedAt []time.Time,
	sourceModifiedAt []time.Time,
) error {
	if len(urls) != len(profiles) ||
		len(urls) != len(fetchedAt) ||
		len(urls) != len(sourceModifiedAt) {
		return fmt.Errorf("record profile fetches: mismatched slice lengths")
	}
	if err := f.persistMissingProfiles(ctx, profiles); err != nil {
		return fmt.Errorf("record profile fetches: store profiles: %w", err)
	}
	observations := make([]fetchObservation, len(urls))
	for index, url := range urls {
		profile := profiles[index]
		observations[index] = fetchObservation{
			url:              url,
			profileHandle:    profile.Handle,
			interval:         profile.RecrawlIfOlder,
			fetchedAt:        fetchedAt[index],
			sourceModifiedAt: sourceModifiedAt[index],
		}
	}
	if err := f.recordFetchBatch(ctx, observations); err != nil {
		return fmt.Errorf("record profile fetches: %w", err)
	}

	return nil
}
