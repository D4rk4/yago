package yagonode

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
	"github.com/D4rk4/yago/yacynode/internal/documentstore"
)

const (
	msgWebSeedFailed   = "web-search crawl seeding failed"
	webSeedProfileName = "web-fallback-seed"
)

// webCrawlSeeder publishes conservative crawl orders for URLs the web-search
// fallback surfaced, so the next identical query can be answered locally. It
// skips URLs already in the document store and relies on the durable queue's
// idempotency (keyed by URL) to avoid re-seeding a recently queued URL.
type webCrawlSeeder struct {
	queue     crawldispatch.CrawlOrderQueue
	documents documentstore.DocumentDirectory
	initiator yacymodel.Hash
	profile   yacycrawlcontract.CrawlProfile
	now       func() time.Time
}

func newWebCrawlSeeder(
	queue crawldispatch.CrawlOrderQueue,
	documents documentstore.DocumentDirectory,
	initiator yacymodel.Hash,
	config webFallbackConfig,
) *webCrawlSeeder {
	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Name:            webSeedProfileName,
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        config.SeedDepth,
		MaxPagesPerHost: config.SeedMaxPages,
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
		order := yacycrawlcontract.CrawlOrder{
			Provenance: mintProvenance(),
			Profile:    s.profile,
			Requests: []yacycrawlcontract.CrawlRequest{{
				URL:           target,
				Mode:          yacycrawlcontract.CrawlRequestModeURL,
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
