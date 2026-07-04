//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlerIsOrderDrivenEndToEnd(t *testing.T) {
	ctx := context.Background()

	network := newNetwork(t, ctx)

	originURL := startOrigin(t, ctx, network.Name)
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
		{URL: originURL, ProfileHandle: order.Profile.Handle},
	}
	exchange.enqueue(t, order)

	batch := exchange.awaitIngest(t)
	if batch.ProfileHandle != order.Profile.Handle {
		t.Errorf("batch handle = %q, want %q", batch.ProfileHandle, order.Profile.Handle)
	}
	if string(batch.Provenance) != "admin" {
		t.Errorf("batch provenance = %q, want admin", batch.Provenance)
	}
}
