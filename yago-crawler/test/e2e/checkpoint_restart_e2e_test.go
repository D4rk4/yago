//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNodeAndCrawlerRestartResumeUnfinishedFrontier(t *testing.T) {
	ctx := context.Background()
	network := newNetwork(t, ctx)
	origin := startRestartOrigin(t, ctx, network.Name)
	node := startNodeBroker(t, ctx, network.Name)
	crawler := startCrawlerForNode(t, ctx, network.Name)

	session := adminLogin(t, ctx, node.opsURL)
	dispatchCrawlWithDepth(t, ctx, node.opsURL, session, origin.seedURL, 1)
	if !waitFor(2*time.Minute, func() bool {
		return indexedDocuments(t, ctx, node.opsURL, session) == 1 &&
			origin.requests(ctx, restartSeedPath) == 1 &&
			origin.requests(ctx, restartPendingStartPath) == 1
	}) {
		t.Fatalf(
			"restart boundary was not reached: documents=%d seedRequests=%d pendingStarts=%d",
			indexedDocuments(t, ctx, node.opsURL, session),
			origin.requests(ctx, restartSeedPath),
			origin.requests(ctx, restartPendingStartPath),
		)
	}

	crashContainer(t, ctx, crawler)
	crashContainer(t, ctx, node.container)
	if err := node.container.Start(ctx); err != nil {
		t.Fatalf("restart node: %v", err)
	}
	node = remapNodeBroker(t, ctx, node)
	if !waitFor(time.Minute, func() bool {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, node.opsURL+"/health", nil)
		if err != nil {
			return false
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return false
		}
		defer func() { _ = response.Body.Close() }()

		return response.StatusCode == http.StatusOK
	}) {
		t.Fatal("restarted node did not become reachable")
	}
	session = adminLogin(t, ctx, node.opsURL)
	if got := indexedDocuments(t, ctx, node.opsURL, session); got != 1 {
		t.Fatalf("documents after node restart = %d, want 1", got)
	}
	if err := crawler.Start(ctx); err != nil {
		t.Fatalf("restart crawler: %v", err)
	}
	if !waitFor(2*time.Minute, func() bool {
		return indexedDocuments(t, ctx, node.opsURL, session) == 2 &&
			origin.requests(ctx, restartPendingStartPath) == 2
	}) {
		t.Fatalf(
			"recovered frontier did not finish: documents=%d seedRequests=%d pendingStarts=%d",
			indexedDocuments(t, ctx, node.opsURL, session),
			origin.requests(ctx, restartSeedPath),
			origin.requests(ctx, restartPendingStartPath),
		)
	}
	if got := origin.requests(ctx, restartSeedPath); got != 1 {
		t.Fatalf("committed seed requests = %d, want 1", got)
	}
	if !waitFor(30*time.Second, func() bool {
		monitor, ok := crawlMonitorBody(ctx, node.opsURL, session)

		return ok && strings.Contains(monitor, "0 pending, 0 leased") &&
			strings.Contains(monitor, ">"+combinedCrawlName+"<") &&
			strings.Contains(monitor, ">finished<")
	}) {
		monitor, _ := crawlMonitorBody(ctx, node.opsURL, session)
		t.Fatalf("terminal crawl state was not reconciled: %s", monitor)
	}

	crashContainer(t, ctx, crawler)
	crashContainer(t, ctx, node.container)
	if err := node.container.Start(ctx); err != nil {
		t.Fatalf("restart settled node: %v", err)
	}
	if err := crawler.Start(ctx); err != nil {
		t.Fatalf("restart settled crawler: %v", err)
	}
	time.Sleep(10 * time.Second)
	if got := origin.requests(ctx, restartSeedPath); got != 1 {
		t.Fatalf("settled seed requests after second restart = %d, want 1", got)
	}
	if got := origin.requests(ctx, restartPendingStartPath); got != 2 {
		t.Fatalf("settled pending starts after second restart = %d, want 2", got)
	}
}
