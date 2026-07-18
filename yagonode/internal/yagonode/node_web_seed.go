package yagonode

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	msgWebSeedFailed   = "web-search crawl seeding failed"
	webSeedProfileName = "web-fallback-seed"
)

// seedProfile names a crawl-seeding source and bounds how far its
// conservative, domain-scoped orders may crawl. options carries the per-crawl
// fetch policy the autocrawler applies to every seeded order.
type seedProfile struct {
	name     string
	depth    int
	maxPages int
	options  seedCrawlOptions
}

// webCrawlSeeder publishes conservative crawl orders for URLs the web-search
// fallback surfaced, so the next identical query can be answered locally. It
// skips URLs already in the document store and relies on the durable queue's
// idempotency (keyed by URL) to avoid re-seeding a recently queued URL.
type webCrawlSeeder struct {
	queue          crawldispatch.CrawlOrderQueue
	documents      documentstore.DocumentDirectory
	initiator      yagomodel.Hash
	profile        yagocrawlcontract.CrawlProfile
	maxPagesPerRun func() int
	now            func() time.Time
}

type webCrawlSeedProfile struct {
	fallback       webFallbackConfig
	crawl          seedCrawlOptions
	maxPagesPerRun func() int
}

func newWebCrawlSeeder(
	queue crawldispatch.CrawlOrderQueue,
	documents documentstore.DocumentDirectory,
	initiator yagomodel.Hash,
	seed webCrawlSeedProfile,
) *webCrawlSeeder {
	return newCrawlSeeder(queue, documents, initiator, seedProfile{
		name:     webSeedProfileName,
		depth:    seed.fallback.SeedDepth,
		maxPages: seed.fallback.SeedMaxPages,
		options:  seed.crawl,
	}, seed.maxPagesPerRun)
}

func newCrawlSeeder(
	queue crawldispatch.CrawlOrderQueue,
	documents documentstore.DocumentDirectory,
	initiator yagomodel.Hash,
	seed seedProfile,
	maxPagesPerRun ...func() int,
) *webCrawlSeeder {
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:                seed.name,
		Scope:               yagocrawlcontract.ScopeDomain,
		URLMustMatch:        yagocrawlcontract.MatchAll,
		MaxDepth:            seed.depth,
		MaxPagesPerHost:     seed.maxPages,
		AllowQueryURLs:      seed.options.AllowQueryURLs,
		IgnoreTLSAuthority:  seed.options.IgnoreTLSAuthority,
		IgnoreRobots:        seed.options.IgnoreRobots,
		DisableBrowser:      seed.options.DisableBrowser,
		FollowNoFollowLinks: seed.options.FollowNoFollowLinks,
		RecrawlIfOlder:      seed.options.RecrawlInterval,
	})

	return &webCrawlSeeder{
		queue:          queue,
		documents:      documents,
		initiator:      initiator,
		profile:        profile,
		maxPagesPerRun: selectMaxPagesPerRunSource(maxPagesPerRun),
		now:            time.Now,
	}
}

func (s *webCrawlSeeder) Seed(ctx context.Context, urls []string) {
	for _, raw := range urls {
		target := seedURL(raw)
		if target == "" || s.stored(ctx, target) {
			continue
		}
		profile := s.profile
		maximum := s.maxPagesPerRun()
		profile.MaxPagesPerRun = &maximum
		profile = yagocrawlcontract.NewCrawlProfile(profile)
		order := yagocrawlcontract.CrawlOrder{
			Provenance: mintProvenance(),
			Priority:   yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
			Profile:    profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           target,
				Mode:          yagocrawlcontract.CrawlRequestModeURL,
				ProfileHandle: profile.Handle,
				Initiator:     s.initiator,
				AppDate:       s.now(),
			}},
		}
		if _, err := s.queue.PublishOnce(ctx, target, order); err != nil {
			slog.DebugContext(ctx, msgWebSeedFailed, slog.Any("error", err))
		}
	}
}

func selectMaxPagesPerRunSource(sources []func() int) func() int {
	if len(sources) == 0 || sources[0] == nil {
		return func() int { return yagocrawlcontract.DefaultMaxPagesPerRun }
	}

	return func() int {
		value := sources[0]()
		if value < 0 {
			return yagocrawlcontract.DefaultMaxPagesPerRun
		}

		return value
	}
}

func (s *webCrawlSeeder) stored(ctx context.Context, target string) bool {
	_, found, err := s.documents.Document(ctx, target)

	return err == nil && found
}

func seedURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !parsed.IsAbs() {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.Fragment = ""

	return parsed.String()
}
