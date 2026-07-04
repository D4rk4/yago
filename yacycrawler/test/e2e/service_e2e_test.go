//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func TestCrawlerIsOrderDrivenEndToEnd(t *testing.T) {
	ctx := context.Background()

	network := newNetwork(t, ctx)

	natsURL := startNATS(t, ctx, network.Name)
	originURL := startOrigin(t, ctx, network.Name)
	startEgressProxy(t, ctx, network.Name)
	startCrawler(t, ctx, network.Name)

	js := connectJetStream(t, natsURL)
	ensureStreams(t, ctx, js)

	order := yacycrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
			Name:            "default",
			Scope:           yacycrawlcontract.ScopeDomain,
			URLMustMatch:    yacycrawlcontract.MatchAll,
			MaxDepth:        0,
			MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		}),
	}
	order.Requests = []yacycrawlcontract.CrawlRequest{
		{URL: originURL, ProfileHandle: order.Profile.Handle},
	}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if _, err := js.Publish(ctx, ordersSubject, data); err != nil {
		t.Fatalf("publish order: %v", err)
	}

	index := fetchOneCrawledPageIndex(t, ctx, js)
	if index.ProfileHandle != order.Profile.Handle {
		t.Errorf("index handle = %q, want %q", index.ProfileHandle, order.Profile.Handle)
	}
	if string(index.Provenance) != "admin" {
		t.Errorf("index provenance = %q, want admin", index.Provenance)
	}
}
