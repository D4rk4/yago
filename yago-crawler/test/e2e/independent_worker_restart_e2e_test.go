//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

var crawlerPulseProcessCell = regexp.MustCompile(
	`<th scope="row">[^<]+</th><td>([^<]+)</td>`,
)

func TestIndependentNodeAndCrawlerRestartsResumeCrawlWork(t *testing.T) {
	ctx := context.Background()
	network := newNetwork(t, ctx)
	origin := startRestartOrigin(t, ctx, network.Name)
	node := startNodeBroker(t, ctx, network.Name)
	crawler := startCrawlerForNode(t, ctx, network.Name)
	session := adminLogin(t, ctx, node.opsURL)

	initialProcess := waitForCrawlerProcess(t, ctx, node.opsURL, session, "")
	dispatchNamedCrawl(
		t,
		ctx,
		node.opsURL,
		session,
		"before-node-restart",
		origin.seedURL,
		0,
	)
	waitForRestartOriginRequests(t, ctx, origin, 1)

	crashContainer(t, ctx, node.container)
	if err := node.container.Start(ctx); err != nil {
		t.Fatalf("restart node while crawler remains running: %v", err)
	}
	node = remapNodeBroker(t, ctx, node)
	if !waitFor(90*time.Second, func() bool {
		return nodeHealthReady(ctx, node.opsURL)
	}) {
		t.Fatal("restarted node did not become reachable")
	}
	session = adminLogin(t, ctx, node.opsURL)
	waitForCrawlerProcess(t, ctx, node.opsURL, session, initialProcess)
	dispatchNamedCrawl(
		t,
		ctx,
		node.opsURL,
		session,
		"after-node-restart",
		origin.seedURL,
		0,
	)
	waitForRestartOriginRequests(t, ctx, origin, 2)

	crashContainer(t, ctx, crawler)
	if err := crawler.Start(ctx); err != nil {
		t.Fatalf("restart crawler while node remains running: %v", err)
	}
	waitForReplacementCrawlerProcess(
		t,
		ctx,
		node.opsURL,
		session,
		initialProcess,
	)
	dispatchNamedCrawl(
		t,
		ctx,
		node.opsURL,
		session,
		"after-crawler-restart",
		origin.seedURL,
		0,
	)
	waitForRestartOriginRequests(t, ctx, origin, 3)
}

func waitForCrawlerProcess(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	previous string,
) string {
	t.Helper()
	var process string
	if !waitFor(90*time.Second, func() bool {
		process = crawlerPulseProcess(ctx, opsURL, session)
		return process != "" && (previous == "" || process == previous)
	}) {
		t.Fatal("expected crawler process session did not become available")
	}

	return process
}

func waitForReplacementCrawlerProcess(
	t *testing.T,
	ctx context.Context,
	opsURL string,
	session adminSession,
	previous string,
) string {
	t.Helper()
	var process string
	if !waitFor(90*time.Second, func() bool {
		process = crawlerPulseProcess(ctx, opsURL, session)
		return process != "" && process != previous
	}) {
		t.Fatal("replacement crawler session did not become available")
	}

	return process
}

func crawlerPulseProcess(ctx context.Context, opsURL string, session adminSession) string {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		opsURL+"/admin/performance",
		nil,
	)
	if err != nil {
		return ""
	}
	request.Header.Set("Cookie", sessionCookie+"="+session.cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), "Connected crawler processes") {
		return ""
	}
	tableOffset := bytes.Index(body, []byte("Crawler processes</caption>"))
	if tableOffset < 0 {
		return ""
	}
	match := crawlerPulseProcessCell.FindSubmatch(body[tableOffset:])
	if len(match) != 2 || string(match[1]) == "Unavailable" {
		return ""
	}

	return string(match[1])
}

func nodeHealthReady(ctx context.Context, opsURL string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, opsURL+"/health", nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer func() { _ = response.Body.Close() }()

	return response.StatusCode == http.StatusOK
}

func waitForRestartOriginRequests(
	t *testing.T,
	ctx context.Context,
	origin restartOrigin,
	want int,
) {
	t.Helper()
	if !waitFor(90*time.Second, func() bool {
		return origin.requests(ctx, restartSeedPath) >= want
	}) {
		t.Fatalf("completed restart-origin fetches below expected minimum %d", want)
	}
}
