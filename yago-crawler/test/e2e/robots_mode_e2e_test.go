//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRobotsModeDiscoversSitemapEndToEnd(t *testing.T) {
	ctx := context.Background()

	network := newNetwork(t, ctx)

	originURL := startRobotsOrigin(t, ctx, network.Name)
	exchangePort, exchange := startExchange(t)
	startCrawler(t, ctx, network.Name, exchangePort)

	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile: yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
			Name:            "default",
			Scope:           yagocrawlcontract.ScopeDomain,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxDepth:        0,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		}),
	}
	order.Requests = []yagocrawlcontract.CrawlRequest{
		{
			URL:           originURL,
			Mode:          yagocrawlcontract.CrawlRequestModeRobots,
			ProfileHandle: order.Profile.Handle,
		},
	}
	exchange.enqueue(t, order)

	batch := exchange.awaitIngest(t)
	if batch.ProfileHandle != order.Profile.Handle {
		t.Errorf("batch handle = %q, want %q", batch.ProfileHandle, order.Profile.Handle)
	}
	if batch.SourceURL != originURL {
		t.Fatalf(
			"ingested SourceURL = %q, want the sitemap-discovered %q",
			batch.SourceURL,
			originURL,
		)
	}
}
