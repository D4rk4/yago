//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

func TestIndependentServiceRestartsRecoverCrawlerSessions(t *testing.T) {
	ctx := context.Background()
	network := newNetwork(t, ctx)
	recovery := independentServiceRestartRecovery{
		t:       t,
		ctx:     ctx,
		origin:  startRestartOrigin(t, ctx, network.Name),
		node:    startNodeBroker(t, ctx, network.Name),
		crawler: startCrawlerForNode(t, ctx, network.Name),
	}
	recovery.session = adminLogin(t, ctx, recovery.node.opsURL)
	recovery.verifyNodeRestart()
	recovery.verifyCrawlerRestart()
}

type independentServiceRestartRecovery struct {
	t       *testing.T
	ctx     context.Context
	origin  restartOrigin
	node    nodeBroker
	crawler testcontainers.Container
	session adminSession
}

func (recovery *independentServiceRestartRecovery) verifyNodeRestart() {
	dispatchNamedCrawl(
		recovery.t,
		recovery.ctx,
		recovery.node.opsURL,
		recovery.session,
		"node-only-restart",
		recovery.origin.seedURL,
		1,
	)
	if !waitFor(2*time.Minute, func() bool {
		return recovery.indexedDocuments() == 1 &&
			recovery.origin.requests(recovery.ctx, restartPendingStartPath) == 1
	}) {
		recovery.t.Fatalf(
			"node-only restart boundary was not reached: documents=%d pendingStarts=%d",
			recovery.indexedDocuments(),
			recovery.origin.requests(recovery.ctx, restartPendingStartPath),
		)
	}
	crashContainer(recovery.t, recovery.ctx, recovery.node.container)
	if err := recovery.node.container.Start(recovery.ctx); err != nil {
		recovery.t.Fatalf("restart node while crawler remains live: %v", err)
	}
	recovery.node = awaitRestartedNode(recovery.t, recovery.ctx, recovery.node)
	recovery.session = adminLogin(recovery.t, recovery.ctx, recovery.node.opsURL)
	if !waitFor(2*time.Minute, func() bool {
		return recovery.indexedDocuments() >= 2
	}) {
		recovery.t.Fatalf(
			"unfinished crawl did not progress after node-only restart: documents=%d pendingStarts=%d",
			recovery.indexedDocuments(),
			recovery.origin.requests(recovery.ctx, restartPendingStartPath),
		)
	}
	dispatchNamedCrawl(
		recovery.t,
		recovery.ctx,
		recovery.node.opsURL,
		recovery.session,
		"node-session-recovered",
		recovery.origin.seedURL+"?node-session=recovered",
		0,
	)
	if !waitFor(time.Minute, func() bool {
		return recovery.indexedDocuments() >= 3
	}) {
		recovery.t.Fatalf(
			"new crawl did not progress after node-only restart: documents=%d",
			recovery.indexedDocuments(),
		)
	}
	assertLeaseLossStopsRepeating(recovery.t, recovery.ctx, recovery.crawler)
}

func (recovery *independentServiceRestartRecovery) verifyCrawlerRestart() {
	pendingStarts := recovery.origin.requests(recovery.ctx, restartPendingStartPath)
	dispatchNamedCrawl(
		recovery.t,
		recovery.ctx,
		recovery.node.opsURL,
		recovery.session,
		"crawler-only-restart",
		"http://"+restartOriginAlias+restartPendingPath+"?crawler-session=replacement",
		0,
	)
	if !waitFor(time.Minute, func() bool {
		return recovery.origin.requests(recovery.ctx, restartPendingStartPath) > pendingStarts
	}) {
		recovery.t.Fatal("crawler-only restart fixture did not begin fetching")
	}
	crashContainer(recovery.t, recovery.ctx, recovery.crawler)
	if err := recovery.crawler.Start(recovery.ctx); err != nil {
		recovery.t.Fatalf("restart crawler while node remains live: %v", err)
	}
	if !waitFor(2*time.Minute, func() bool {
		return recovery.indexedDocuments() >= 4
	}) {
		recovery.t.Fatalf(
			"replacement crawler session did not resume unfinished work: documents=%d",
			recovery.indexedDocuments(),
		)
	}
	dispatchNamedCrawl(
		recovery.t,
		recovery.ctx,
		recovery.node.opsURL,
		recovery.session,
		"crawler-session-recovered",
		recovery.origin.seedURL+"?crawler-session=recovered",
		0,
	)
	if !waitFor(time.Minute, func() bool {
		return recovery.indexedDocuments() >= 5
	}) {
		recovery.t.Fatalf(
			"new crawl did not progress through replacement crawler session: documents=%d",
			recovery.indexedDocuments(),
		)
	}
	assertLeaseLossStopsRepeating(recovery.t, recovery.ctx, recovery.crawler)
}

func (recovery *independentServiceRestartRecovery) indexedDocuments() int {
	return indexedDocuments(
		recovery.t,
		recovery.ctx,
		recovery.node.opsURL,
		recovery.session,
	)
}

func awaitRestartedNode(
	t *testing.T,
	ctx context.Context,
	node nodeBroker,
) nodeBroker {
	t.Helper()
	node = remapNodeBroker(t, ctx, node)
	if !waitFor(time.Minute, func() bool {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			node.opsURL+"/health",
			nil,
		)
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

	return node
}

func assertLeaseLossStopsRepeating(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
) {
	t.Helper()
	before := containerLogOccurrences(t, ctx, container, "crawl lease lost")
	time.Sleep(7 * time.Second)
	after := containerLogOccurrences(t, ctx, container, "crawl lease lost")
	if after != before {
		t.Fatalf("crawl lease loss repeated after recovery: before=%d after=%d", before, after)
	}
}

func containerLogOccurrences(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	text string,
) int {
	t.Helper()
	reader, err := container.Logs(ctx)
	if err != nil {
		t.Fatalf("read container logs: %v", err)
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("consume container logs: %v", err)
	}

	return strings.Count(string(raw), text)
}
