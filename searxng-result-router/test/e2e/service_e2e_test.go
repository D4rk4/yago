//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/dockernetwork"
	"github.com/nikitakarpei/yacy-rwi-node/e2eharness/natsjetstream"
)

func TestResultLinkRouterRoutesSearchResultsThroughVisitcrawl(t *testing.T) {
	ctx := context.Background()

	network := dockernetwork.New(t, ctx)

	natsURL := natsjetstream.Start(t, ctx, network.Name)
	startOrigin(t, ctx, network.Name)
	visitcrawlBaseURL := startVisitcrawl(t, ctx, network.Name)
	searxngBaseURL := startSearXNG(t, ctx, network.Name, visitcrawlBaseURL)

	js := connectJetStream(t, natsURL)
	ensureOrdersStream(t, ctx, js)

	result := searchOneResult(t, ctx, searxngBaseURL, "!"+testEngineBang+" test")

	wantPrefix := visitcrawlBaseURL + "/visit?url="
	if !strings.HasPrefix(result.URL, wantPrefix) {
		t.Fatalf("result url = %q, want prefix %q", result.URL, wantPrefix)
	}
	gotDestination, err := url.QueryUnescape(strings.TrimPrefix(result.URL, wantPrefix))
	if err != nil {
		t.Fatalf("decode routed url: %v", err)
	}
	if gotDestination != originDestination {
		t.Fatalf("routed destination = %q, want %q", gotDestination, originDestination)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.URL, nil)
	if err != nil {
		t.Fatalf("build visit request: %v", err)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("visit request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("visit status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if location := resp.Header.Get("Location"); location != originDestination {
		t.Fatalf("visit redirect location = %q, want %q", location, originDestination)
	}

	order := fetchOnePlacedOrder(t, ctx, js)
	if len(order.SeedURLs) != 1 || order.SeedURLs[0] != originDestination {
		t.Fatalf("placed order seed urls = %v, want [%q]", order.SeedURLs, originDestination)
	}
}
