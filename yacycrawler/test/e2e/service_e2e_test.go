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
		OrderID: "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
		Profile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
			Name:            "default",
			Scope:           yacycrawlcontract.ScopeDomain,
			URLMustMatch:    yacycrawlcontract.MatchAll,
			MaxDepth:        0,
			MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		}),
		SeedURLs: []string{originURL},
	}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if _, err := js.Publish(ctx, ordersSubject, data); err != nil {
		t.Fatalf("publish order: %v", err)
	}

	index := fetchOneCrawledPageIndex(t, ctx, js)
	if index.CanonicalURL != originURL {
		t.Errorf("index canonical url = %q, want %q", index.CanonicalURL, originURL)
	}
	if len(index.Postings) == 0 {
		t.Error("index carries no postings")
	}
}
