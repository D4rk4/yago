// Package bootstrap fetches the configured YaCy seed lists and returns the seeds
// they advertise. It is the cold-start seed source the peer roster consults when
// it holds no peers yet; once the roster is populated it is no longer needed.
package bootstrap

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type SeedSource interface {
	Fetch(ctx context.Context) []yacymodel.Seed
}

type SeedlistSource struct {
	fetcher httpSeedlistFetcher
	urls    []string
}

var _ SeedSource = (*SeedlistSource)(nil)

func New(client *http.Client, urls []string) *SeedlistSource {
	return &SeedlistSource{fetcher: newHTTPSeedlistFetcher(client), urls: urls}
}

func (s *SeedlistSource) Fetch(ctx context.Context) []yacymodel.Seed {
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
