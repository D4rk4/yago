//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"
)

// TestNodeBrokerDrivesCrawlerToIndex exercises the whole control plane with real
// binaries: a node container (durable lease broker + admin API), a crawler
// container that streams and leases orders from it, and an origin to fetch. It
// dispatches one crawl through the node's authenticated admin API and asserts the
// crawled page becomes an indexed, searchable document — proving the order,
// lease, stream, fetch, ingest, index path end to end over the gRPC wire.
func TestNodeBrokerDrivesCrawlerToIndex(t *testing.T) {
	ctx := context.Background()

	network := newNetwork(t, ctx)
	originURL := startOrigin(t, ctx, network.Name)
	node := startNodeBroker(t, ctx, network.Name)
	startCrawlerForNode(t, ctx, network.Name)

	session := adminLogin(t, ctx, node.opsURL)
	dispatchCrawl(t, ctx, node.opsURL, session, originURL)

	if !waitFor(2*time.Minute, func() bool {
		return indexedDocuments(t, ctx, node.opsURL, session) > 0
	}) {
		t.Logf("index stats: %s", rawGet(ctx, node.opsURL+pathIndexStats, session.cookie))
		t.Fatal("dispatched crawl never produced an indexed document on the node")
	}

	if !waitFor(30*time.Second, func() bool {
		return searchFindsTerm(ctx, node.publicURL, "words")
	}) {
		t.Logf("search: %s", rawGet(ctx, node.publicURL+pathSearchJSON+"?query=words", ""))
		t.Fatal("indexed document was not returned by a body-term search")
	}
	if !rankingExplainFindsTerm(ctx, node.opsURL, session, "words") {
		t.Fatal("YagoRank explain did not expose field evidence for the indexed document")
	}
	if !rankingModelIsInactive(ctx, node.opsURL, session) {
		t.Fatal("fresh node did not report an inactive learned ranking model")
	}
	if !rankingTrainingRejectsColdStart(ctx, node.opsURL, session) {
		t.Fatal("ranking training did not reject an empty judgment corpus")
	}
	if !rankingModelIsInactive(ctx, node.opsURL, session) {
		t.Fatal("rejected training changed the active ranking model")
	}
}
