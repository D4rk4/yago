package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const swarmSeedProfileName = "swarm-seed"

const (
	swarmSeedConcurrentWrites = 2
	swarmSeedWriteTimeout     = 10 * time.Second
)

// urlSeeder enqueues conservative crawl orders for search-surfaced URLs.
type urlSeeder interface {
	Seed(ctx context.Context, urls []string)
}

// swarmSeedingSearcher enqueues bounded crawls of URLs surfaced by swarm
// search — YaCy's greedy learning: a fresh peer grows a useful index from what
// the network already answers with. Seeding stays active for the life of the
// node (there is no document-count ceiling), so even a large index keeps
// discovering resources that neither it nor the swarm already holds instead of
// silently switching greedy learning off once it fills up. It runs off the
// request path.
type swarmSeedingSearcher struct {
	inner  searchcore.Searcher
	seeder urlSeeder
	spawn  func(func()) bool
}

func withSwarmSeedCrawl(
	inner searchcore.Searcher,
	seeder urlSeeder,
) searchcore.Searcher {
	admission := newSwarmSeedAdmission(swarmSeedConcurrentWrites)

	return swarmSeedingSearcher{
		inner:  inner,
		seeder: seeder,
		spawn:  admission.try,
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
	if len(remote) == 0 {
		return resp, nil
	}
	urls := make([]string, 0, len(remote))
	for _, result := range remote {
		urls = append(urls, result.URL)
	}
	seedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), swarmSeedWriteTimeout)
	if !s.spawn(func() {
		defer cancel()
		s.seeder.Seed(seedCtx, urls)
	}) {
		cancel()
	}

	return resp, nil
}
