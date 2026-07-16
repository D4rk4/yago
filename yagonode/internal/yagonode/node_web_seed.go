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
	queue     crawldispatch.CrawlOrderQueue
	documents documentstore.DocumentDirectory
	initiator yagomodel.Hash
	profile   yagocrawlcontract.CrawlProfile
	now       func() time.Time
}

func newWebCrawlSeeder(
	queue crawldispatch.CrawlOrderQueue,
	documents documentstore.DocumentDirectory,
	initiator yagomodel.Hash,
	config webFallbackConfig,
	options seedCrawlOptions,
) *webCrawlSeeder {
	return newCrawlSeeder(queue, documents, initiator, seedProfile{
		name:     webSeedProfileName,
		depth:    config.SeedDepth,
		maxPages: config.SeedMaxPages,
		options:  options,
	})
}

func newCrawlSeeder(
	queue crawldispatch.CrawlOrderQueue,
	documents documentstore.DocumentDirectory,
	initiator yagomodel.Hash,
	seed seedProfile,
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
		queue:     queue,
		documents: documents,
		initiator: initiator,
		profile:   profile,
		now:       time.Now,
	}
}

func (s *webCrawlSeeder) Seed(ctx context.Context, urls []string) {
	for _, raw := range urls {
		target := seedURL(raw)
		if target == "" || s.stored(ctx, target) {
			continue
		}
		order := yagocrawlcontract.CrawlOrder{
			Provenance: mintProvenance(),
			Priority:   yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
			Profile:    s.profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           target,
				Mode:          yagocrawlcontract.CrawlRequestModeURL,
				ProfileHandle: s.profile.Handle,
				Initiator:     s.initiator,
				AppDate:       s.now(),
			}},
		}
		if _, err := s.queue.PublishOnce(ctx, target, order); err != nil {
			slog.DebugContext(ctx, msgWebSeedFailed, slog.Any("error", err))
		}
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
