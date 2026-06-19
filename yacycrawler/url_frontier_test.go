package yacycrawler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
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
	frontier.Seed(ctx, reqs, []byte("test"))
	return nil
}

func TestFrontierFollowsLinksWithinDepthAndHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/a">a</a><a href="/b">b</a>`+
			`<a href="http://elsewhere.invalid/x">off</a>`)
	})
	mux.HandleFunc("/a", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/c">c</a><a href="/b">b again</a>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `leaf`)
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/deep">deep</a>`)
	})
	mux.HandleFunc("/deep", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("requested /deep beyond max depth")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
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

	if err := seedCrawl(ctx, frontier, registry, 2, server.URL); err != nil {
		t.Fatalf("seed: %v", err)
	}
	<-workersDone
	ingest.Close()
	<-nodeDone

	visited := map[string]bool{}
	for _, batch := range node.Batches() {
		visited[batch.SourceURL] = true
	}
	for _, want := range []string{server.URL, server.URL + "/a", server.URL + "/b", server.URL + "/c"} {
		if !visited[want] {
			t.Errorf("expected %s to be crawled, visited=%v", want, visited)
		}
	}
	if len(node.Batches()) != 4 {
		t.Errorf("expected 4 unique pages, got %d: %v", len(node.Batches()), visited)
	}
}

func TestFrontierProfileFiltersLinks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/keep">keep</a>`+
			`<a href="/skip-me">skip</a>`+
			`<a href="/query?x=1">query</a>`)
	})
	mux.HandleFunc("/keep", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `kept`)
	})
	mux.HandleFunc("/skip-me", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("requested /skip-me excluded by URLMustNotMatch")
	})
	mux.HandleFunc("/query", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("requested query URL with AllowQueryURLs=false")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
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
	}, server.URL)
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
	if !visited[server.URL] || !visited[server.URL+"/keep"] {
		t.Errorf("expected root and /keep crawled, visited=%v", visited)
	}
	if len(node.Batches()) != 2 {
		t.Errorf("expected 2 pages (root + keep), got %d: %v", len(node.Batches()), visited)
	}
}

func writeHTML(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte("<html><body>" + body + "</body></html>")); err != nil {
		t.Errorf("write body: %v", err)
	}
}
