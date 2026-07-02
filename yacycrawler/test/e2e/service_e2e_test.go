//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yacycrawlcontract"
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

	batch := fetchOneIngest(t, ctx, js)
	if batch.ProfileHandle != order.Profile.Handle {
		t.Errorf("batch handle = %q, want %q", batch.ProfileHandle, order.Profile.Handle)
	}
	if string(batch.Provenance) != "admin" {
		t.Errorf("batch provenance = %q, want admin", batch.Provenance)
	}
}
