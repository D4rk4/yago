package websearch

import "context"

// CrawlSeeder hands URLs discovered by the web-search fallback to the crawler so
// the next identical query can be answered from the local index. Implementations
// must be best-effort and non-fatal: a seeding failure never fails the search.
type CrawlSeeder interface {
	Seed(ctx context.Context, urls []string)
}

// Option configures a FallbackSearcher.
type Option func(*FallbackSearcher)

// WithSeeder installs a crawl seeder that receives the fallback's result URLs.
func WithSeeder(seeder CrawlSeeder) Option {
	return func(searcher *FallbackSearcher) { searcher.seeder = seeder }
}
