package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

// countingSource is a page source that serves fixed bodies by path and records
// how many times each path is fetched, so a test can assert the frontier never
// dispatches a URL twice under concurrent workers.
type countingSource struct {
	mu      sync.Mutex
	fetches map[string]int
	pages   map[string]string
}

func newCountingSource(pages map[string]string) *countingSource {
	return &countingSource{fetches: make(map[string]int), pages: pages}
}

func (s *countingSource) Fetch(
	_ context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	path := target.Path
	if path == "" {
		path = "/"
	}
	body, ok := s.pages[path]
	if !ok {
		return pagefetch.FetchedPage{}, fmt.Errorf("missing test page: %s", path)
	}
	s.mu.Lock()
	s.fetches[path]++
	s.mu.Unlock()

	return pagefetch.FetchedPage{
		URL:         target,
		ContentType: "text/html",
		Body:        []byte("<html><body>" + body + "</body></html>"),
	}, nil
}

func (s *countingSource) snapshot() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.fetches))
	for path, n := range s.fetches {
		out[path] = n
	}

	return out
}

func (s *countingSource) distinct() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.fetches)
}

func flowConfig(workers int) ServiceConfig {
	cfg := serviceConfig()
	cfg.Crawl.Workers = workers
	cfg.Crawl.CrawlDelay = 0
	cfg.Crawl.MaxHostConcurrency = 0
	cfg.Crawl.MaxDepth = 3

	return cfg
}

func linkedOrderMessage(t *testing.T, target string, maxDepth int) *crawlrpc.CrawlOrderMessage {
	t.Helper()
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "flow",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        maxDepth,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("flow-run"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{
			{URL: target, ProfileHandle: profile.Handle},
		},
	}
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}

	return &crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: "flow-lease"}
}

func awaitFinishedReport(
	t *testing.T,
	progress <-chan *crawlrpc.CrawlProgressReport,
) *crawlrpc.CrawlProgressReport {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case report := <-progress:
			if report.GetState() == crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED {
				return report
			}
		case <-timeout:
			t.Fatal("crawl run never reported finished")

			return nil
		}
	}
}

func loopbackOrigin(t *testing.T) *httptest.Server {
	t.Helper()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fast client forbidden", http.StatusForbidden)
	}))
	t.Cleanup(origin.Close)

	return origin
}

// TestRunServiceMultipleWorkersFetchEachURLOnce drives the fully assembled crawler
// with eight workers over a cross-linked page set where every page re-discovers the
// whole set. The frontier's per-run dedup must ensure each URL is fetched exactly
// once despite the concurrent re-submission, and the finished run report must carry
// the accumulated outcome tally.
func TestRunServiceMultipleWorkersFetchEachURLOnce(t *testing.T) {
	restoreAssemblySeams(t)
	serveViaSlowSource(t)
	origin := loopbackOrigin(t)

	links := `<a href="/">home</a><a href="/a">a</a>` +
		`<a href="/b">b</a><a href="/c">c</a><a href="/d">d</a> words here`
	source := newCountingSource(map[string]string{
		"/": links, "/a": links, "/b": links, "/c": links, "/d": links,
	})

	exchange := &fakeExchange{
		orders:   []*crawlrpc.CrawlOrderMessage{linkedOrderMessage(t, origin.URL+"/", 3)},
		ingested: make(chan *crawlrpc.IngestBatchMessage, 64),
		progress: make(chan *crawlrpc.CrawlProgressReport, 128),
	}
	stubExchange(t, exchange)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Drain ingest freely so a slow node never enters into this assertion.
	go func() {
		for {
			select {
			case <-exchange.ingested:
			case <-ctx.Done():
				return
			}
		}
	}()

	runDone := make(chan error, 1)
	go func() { runDone <- RunService(ctx, flowConfig(8), source) }()

	finished := awaitFinishedReport(t, exchange.progress)

	for path, n := range source.snapshot() {
		if n != 1 {
			t.Errorf("path %s fetched %d times, want exactly 1", path, n)
		}
	}
	if got := source.distinct(); got != 5 {
		t.Fatalf("distinct pages fetched = %d, want 5 (home + a..d)", got)
	}
	if fetched := finished.GetTally().GetFetched(); fetched != 5 {
		t.Errorf("finished tally fetched = %d, want 5", fetched)
	}
	if dups := finished.GetTally().GetDuplicates(); dups == 0 {
		t.Errorf("finished tally duplicates = 0, want the re-discovered visits counted")
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("service did not shut down after cancel")
	}
}

// TestRunServiceBackpressureFromNodeStallsCrawler holds the node's ingest intake
// (an undrained, capacity-1 SubmitIngest channel) and asserts the crawler cannot
// fetch the whole page set while ingest is blocked — the blocking Emit stalls the
// workers. Once the node resumes absorbing batches, the crawl drains fully.
func TestRunServiceBackpressureFromNodeStallsCrawler(t *testing.T) {
	restoreAssemblySeams(t)
	serveViaSlowSource(t)
	origin := loopbackOrigin(t)

	const leaves = 15
	pages := map[string]string{}
	home := ""
	for i := range leaves {
		path := fmt.Sprintf("/p%d", i)
		home += fmt.Sprintf(`<a href="%s">p%d</a>`, path, i)
		pages[path] = "words here"
	}
	pages["/"] = home + " words here"
	totalPages := leaves + 1

	source := newCountingSource(pages)
	exchange := &fakeExchange{
		orders:   []*crawlrpc.CrawlOrderMessage{linkedOrderMessage(t, origin.URL+"/", 2)},
		ingested: make(chan *crawlrpc.IngestBatchMessage, 1),
		progress: make(chan *crawlrpc.CrawlProgressReport, 128),
	}
	stubExchange(t, exchange)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- RunService(ctx, flowConfig(4), source) }()

	// Phase 1 — ingest is never drained, so SubmitIngest blocks and the workers
	// stall at Emit. Give the crawler time to reach that steady state, then confirm
	// it has NOT fetched the whole set.
	time.Sleep(500 * time.Millisecond)
	stalled := source.distinct()
	if stalled < 1 {
		t.Fatal("crawler fetched nothing; expected it to start before stalling")
	}
	if stalled >= totalPages {
		t.Fatalf("crawler fetched %d of %d pages while ingest was blocked; "+
			"backpressure did not stall it", stalled, totalPages)
	}

	// Phase 2 — the node resumes absorbing ingest; the crawl must now drain fully.
	go func() {
		for {
			select {
			case <-exchange.ingested:
			case <-ctx.Done():
				return
			}
		}
	}()

	awaitFinishedReport(t, exchange.progress)
	for path, n := range source.snapshot() {
		if n != 1 {
			t.Errorf("path %s fetched %d times, want exactly 1", path, n)
		}
	}
	if got := source.distinct(); got != totalPages {
		t.Fatalf("distinct pages fetched after drain = %d, want %d", got, totalPages)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("service did not shut down after cancel")
	}
}
