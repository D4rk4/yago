//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/dockernetwork"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/egressproxy"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/natsjetstream"
)

func TestCrawledPageIsSearchableInElasticsearch(t *testing.T) {
	ctx := context.Background()

	network := dockernetwork.New(t, ctx)

	natsURL := natsjetstream.Start(t, ctx, network.Name)
	originURL := startOrigin(t, ctx, network.Name)
	elasticsearchURL := startElasticsearch(t, ctx, network.Name)
	egressproxy.Start(t, ctx, network.Name)
	startNode(t, ctx, network.Name)
	startCrawler(t, ctx, network.Name)
	startTextIndexer(t, ctx, network.Name, elasticsearchTextIndexerEnv())

	js := connectJetStream(t, natsURL)
	ensureStreams(t, ctx, js)

	publishCrawlOrder(t, ctx, js, originURL)

	hit := waitForElasticsearchIndexedHit(t, ctx, elasticsearchURL, originURL)
	if hit.Source.Title != originTitle {
		t.Errorf("indexed title = %q, want %q", hit.Source.Title, originTitle)
	}
	if !strings.Contains(hit.Source.Content, originBody) {
		t.Errorf("indexed content = %q, want it to contain %q", hit.Source.Content, originBody)
	}
}

func TestCrawledPageIsSearchableInManticore(t *testing.T) {
	ctx := context.Background()

	network := dockernetwork.New(t, ctx)

	natsURL := natsjetstream.Start(t, ctx, network.Name)
	originURL := startOrigin(t, ctx, network.Name)
	manticoreURL := startManticore(t, ctx, network.Name)
	egressproxy.Start(t, ctx, network.Name)
	startNode(t, ctx, network.Name)
	startCrawler(t, ctx, network.Name)
	startTextIndexer(t, ctx, network.Name, manticoreTextIndexerEnv())

	js := connectJetStream(t, natsURL)
	ensureStreams(t, ctx, js)

	publishCrawlOrder(t, ctx, js, originURL)

	hit := waitForManticoreIndexedHit(t, ctx, manticoreURL, originURL)
	if hit.Source.Title != originTitle {
		t.Errorf("indexed title = %q, want %q", hit.Source.Title, originTitle)
	}
	if !strings.Contains(hit.Source.Content, originBody) {
		t.Errorf("indexed content = %q, want it to contain %q", hit.Source.Content, originBody)
	}
}
