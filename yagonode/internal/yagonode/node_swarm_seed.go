package yagonode

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const swarmSeedProfileName = "swarm-seed"

// urlSeeder enqueues conservative crawl orders for search-surfaced URLs.
type urlSeeder interface {
	Seed(ctx context.Context, urls []string)
}

// swarmSeedingSearcher enqueues bounded crawls of URLs surfaced by swarm
// search, YaCy's greedy learning: a fresh peer grows a useful index from what
// the network already answers with, until the local document count reaches
// the configured limit (YaCy greedylearning.limit.doccount, after which
// greedylearning.active turns off). Seeding runs off the request path.
type swarmSeedingSearcher struct {
	inner     searchcore.Searcher
	seeder    urlSeeder
	documents documentstore.DocumentDirectory
	limitDocs int
	spawn     func(func())
}

func withSwarmSeedCrawl(
	inner searchcore.Searcher,
	seeder urlSeeder,
	documents documentstore.DocumentDirectory,
	limitDocs int,
) searchcore.Searcher {
	return swarmSeedingSearcher{
		inner:     inner,
		seeder:    seeder,
		documents: documents,
		limitDocs: limitDocs,
		spawn:     func(work func()) { go work() },
	}
}

func (s swarmSeedingSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.inner.Search(ctx, req)
	if err != nil {
		//nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
		return resp, err
	}
	remote := remoteResults(resp.Results)
	if len(remote) == 0 || !s.underDocumentLimit(ctx) {
		return resp, nil
	}
	urls := make([]string, 0, len(remote))
	for _, result := range remote {
		urls = append(urls, result.URL)
	}
	seedCtx := context.WithoutCancel(ctx)
	s.spawn(func() { s.seeder.Seed(seedCtx, urls) })

	return resp, nil
}

// underDocumentLimit reports whether greedy learning is still growing the
// index; a count failure skips seeding rather than the search.
func (s swarmSeedingSearcher) underDocumentLimit(ctx context.Context) bool {
	count, err := s.documents.Count(ctx)
	if err != nil {
		slog.DebugContext(ctx, "swarm seed crawl skipped", slog.Any("error", err))

		return false
	}

	return count < s.limitDocs
}
