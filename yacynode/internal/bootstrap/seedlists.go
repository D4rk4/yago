package bootstrap

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yacymodel"
)

type seedlists struct {
	fetcher httpSeedlistFetcher
	urls    []string
}

var _ SeedSource = (*seedlists)(nil)

func (s *seedlists) Fetch(ctx context.Context) []yacymodel.Seed {
	var seeds []yacymodel.Seed
	for _, url := range s.urls {
		fetched, err := s.fetcher.Fetch(ctx, url)
		if err != nil {
			slog.WarnContext(
				ctx,
				"seedlist fetch failed",
				slog.String("url", url),
				slog.Any("error", err),
			)

			continue
		}
		seeds = append(seeds, fetched...)
	}

	return seeds
}
