package yacycrawler_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
)

func seedCrawl(
	ctx context.Context,
	frontier *yacycrawler.Frontier,
	registry *yacycrawler.CrawlProfileRegistry,
	maxDepth int,
	seeds ...string,
) error {
	return seedProfile(ctx, frontier, registry, yacycrawlcontract.CrawlProfile{
		Name:            "test",
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        maxDepth,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	}, seeds...)
}

func seedProfile(
	ctx context.Context,
	frontier *yacycrawler.Frontier,
	registry *yacycrawler.CrawlProfileRegistry,
	profile yacycrawlcontract.CrawlProfile,
	seeds ...string,
) error {
	profile = yacycrawlcontract.NewCrawlProfile(profile)
	if err := registry.Register(profile); err != nil {
		return fmt.Errorf("register profile: %w", err)
	}
	reqs := make([]yacycrawlcontract.CrawlRequest, 0, len(seeds))
	for _, s := range seeds {
		reqs = append(reqs, yacycrawlcontract.CrawlRequest{URL: s, ProfileHandle: profile.Handle})
	}
	frontier.Hold()
	frontier.SeedRun(ctx, reqs, []byte("test"), frontier.Release)
	return nil
}

func TestFrontierFollowsLinksWithinDepthAndHost(t *testing.T) {
	baseURL := "http://example.test"

	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := htmlPageSource(map[string]string{
		"/": `<a href="/a">a</a><a href="/b">b</a>` +
			`<a href="http://elsewhere.invalid/x">off</a>`,
		"/a": `<a href="/c">c</a><a href="/b">b again</a>`,
		"/b": `leaf`,
		"/c": `<a href="/deep">deep</a>`,
	})
	publisher := yacycrawler.NewIngestPublisher(ingest)
	registry := yacycrawler.NewCrawlProfileRegistry()
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 3)
		close(workersDone)
	}()

	if err := seedCrawl(ctx, frontier, registry, 2, baseURL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	<-workersDone
	ingest.Close()
	<-nodeDone

	visited := map[string]bool{}
	for _, batch := range node.Batches() {
		visited[batch.SourceURL] = true
	}
	for _, want := range []string{baseURL, baseURL + "/a", baseURL + "/b", baseURL + "/c"} {
		if !visited[want] {
			t.Errorf("expected %s to be crawled, visited=%v", want, visited)
		}
	}
	if len(node.Batches()) != 4 {
		t.Errorf("expected 4 unique pages, got %d: %v", len(node.Batches()), visited)
	}
}

func TestFrontierProfileFiltersLinks(t *testing.T) {
	baseURL := "http://example.test"

	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := htmlPageSource(map[string]string{
		"/":     `<a href="/keep">keep</a><a href="/skip-me">skip</a><a href="/query?x=1">query</a>`,
		"/keep": `kept`,
	})
	publisher := yacycrawler.NewIngestPublisher(ingest)
	registry := yacycrawler.NewCrawlProfileRegistry()
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 3)
		close(workersDone)
	}()

	err := seedProfile(ctx, frontier, registry, yacycrawlcontract.CrawlProfile{
		Name:            "filtered",
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		URLMustNotMatch: ".*/skip-me$",
		MaxDepth:        2,
		AllowQueryURLs:  false,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	}, baseURL)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	<-workersDone
	ingest.Close()
	<-nodeDone

	visited := map[string]bool{}
	for _, batch := range node.Batches() {
		visited[batch.SourceURL] = true
	}
	if !visited[baseURL] || !visited[baseURL+"/keep"] {
		t.Errorf("expected root and /keep crawled, visited=%v", visited)
	}
	if len(node.Batches()) != 2 {
		t.Errorf("expected 2 pages (root + keep), got %d: %v", len(node.Batches()), visited)
	}
}

func TestFrontierAppliesHostLimitToSeeds(t *testing.T) {
	baseURL := "http://example.test"

	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := pageSourceFunc(func(_ context.Context, rawURL string) (yacycrawler.FetchedPage, error) {
		return yacycrawler.FetchedPage{
			URL:         rawURL,
			ContentType: "text/html",
			Body:        []byte(`<html><body>seed</body></html>`),
		}, nil
	})
	publisher := yacycrawler.NewIngestPublisher(ingest)
	registry := yacycrawler.NewCrawlProfileRegistry()
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 2)
		close(workersDone)
	}()

	err := seedProfile(ctx, frontier, registry, yacycrawlcontract.CrawlProfile{
		Name:            "limited",
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: 1,
	}, baseURL+"/a", baseURL+"/b")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	<-workersDone
	ingest.Close()
	<-nodeDone

	if len(node.Batches()) != 1 {
		t.Errorf("got %d batches, want 1", len(node.Batches()))
	}
}

func TestFrontierScopesVisitedURLsByRun(t *testing.T) {
	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := pageSourceFunc(func(_ context.Context, rawURL string) (yacycrawler.FetchedPage, error) {
		return yacycrawler.FetchedPage{
			URL:         rawURL,
			ContentType: "text/html",
			Body:        []byte(`<html><body>seed</body></html>`),
		}, nil
	})
	publisher := yacycrawler.NewIngestPublisher(ingest)
	registry := yacycrawler.NewCrawlProfileRegistry()
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := newFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 2)
		close(workersDone)
	}()

	frontier.Hold()
	if err := seedCrawl(ctx, frontier, registry, 0, "http://example.test/"); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := seedCrawl(ctx, frontier, registry, 0, "http://example.test/"); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	frontier.Release()
	<-workersDone
	ingest.Close()
	<-nodeDone

	if len(node.Batches()) != 2 {
		t.Errorf("got %d batches, want 2", len(node.Batches()))
	}
}
