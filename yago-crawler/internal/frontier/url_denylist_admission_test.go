package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type blockingURLDenylist map[string]bool

func (d blockingURLDenylist) Blocks(rawURL string) bool {
	return d[rawURL]
}

func TestFrontierRejectsDenylistedSeedsAndDiscoveredLinks(t *testing.T) {
	denylist := blockingURLDenylist{
		"https://example.com/blocked-seed": true,
		"https://example.com/blocked-link": true,
	}
	crawlFrontier := frontier.NewFrontier(
		8,
		nil,
		frontier.WithURLDenylist(denylist),
	)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := crawlFrontier.SeedRun(
		context.Background(),
		requestsFor(
			profile.Profile.Handle,
			"https://example.com/blocked-seed",
			"https://example.com/allowed",
		),
		nil,
		profile,
		func(bool) {},
	)
	if seeded.Queued != 1 {
		t.Fatalf("queued seeds = %d, want 1", seeded.Queued)
	}
	root := receiveJob(t, crawlFrontier)
	crawlFrontier.Submit(
		t.Context(),
		root,
		discoveredLinks(
			"https://example.com/blocked-link",
			"https://example.com/allowed-link",
		),
	)
	child := receiveJob(t, crawlFrontier)
	if child.URL != "https://example.com/allowed-link" {
		t.Fatalf("child URL = %q", child.URL)
	}
	crawlFrontier.Done(child, successfulPageOutcome())
	crawlFrontier.Done(root, successfulPageOutcome())
	assertNoJob(t, crawlFrontier, 50*time.Millisecond)
}
