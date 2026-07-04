package bootstrap

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
)

type seedlists struct {
	fetcher  httpSeedlistFetcher
	urls     []string
	observer SeedImportObserver
}

var _ SeedSource = (*seedlists)(nil)

func (s *seedlists) Fetch(ctx context.Context) []yagomodel.Seed {
	var seeds []yagomodel.Seed
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
		if s.observer != nil {
			s.observer.ObserveSeedlistImport(len(fetched))
		}
		seeds = append(seeds, fetched...)
	}

	return seeds
}
