package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagocrawler/internal/crawldelay"
	"github.com/D4rk4/yago/yagocrawler/internal/httpfetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/publicweb"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
	"github.com/D4rk4/yago/yagoegress"
)

type fakeExchange struct {
	orders    []*crawlrpc.CrawlOrderMessage
	ingested  chan *crawlrpc.IngestBatchMessage
	progress  chan *crawlrpc.CrawlProgressReport
	streamErr error
}

func (f *fakeExchange) StreamOrders(
	ctx context.Context,
	_ *crawlrpc.WorkerRegistration,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}

	return &fakeOrderClientStream{ctx: ctx, orders: f.orders}, nil
}

func (f *fakeExchange) SubmitIngest(
	ctx context.Context,
	in *crawlrpc.IngestBatchMessage,
	_ ...grpc.CallOption,
) (*crawlrpc.IngestAck, error) {
	select {
	case f.ingested <- in:
	case <-ctx.Done():
		return nil, fmt.Errorf("submit ingest: %w", ctx.Err())
	}

	return &crawlrpc.IngestAck{}, nil
}

func (f *fakeExchange) AckOrder(
	context.Context,
	*crawlrpc.OrderAck,
	...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	return &crawlrpc.OrderAckResult{}, nil
}

func (f *fakeExchange) Heartbeat(
	context.Context,
	*crawlrpc.WorkerHeartbeat,
	...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return &crawlrpc.WorkerHeartbeatResult{}, nil
}

func (f *fakeExchange) ReportProgress(
	_ context.Context,
	in *crawlrpc.CrawlProgressReport,
	_ ...grpc.CallOption,
) (*crawlrpc.CrawlProgressAck, error) {
	if f.progress != nil {
		f.progress <- in
	}

	return &crawlrpc.CrawlProgressAck{}, nil
}

type fakeOrderClientStream struct {
	grpc.ClientStream
	ctx    context.Context
	orders []*crawlrpc.CrawlOrderMessage
	index  int
}

func (s *fakeOrderClientStream) Recv() (*crawlrpc.CrawlOrderMessage, error) {
	if s.index < len(s.orders) {
		msg := s.orders[s.index]
		s.index++

		return msg, nil
	}
	<-s.ctx.Done()

	return nil, io.EOF
}

func (s *fakeOrderClientStream) Context() context.Context { return s.ctx }

func restoreAssemblySeams(t *testing.T) {
	t.Helper()
	savedExchange := newCrawlerExchange
	savedRobots := newCrawlerRobotsAdmissionFetcher
	savedHTTP := newCrawlerHTTPPageFetcher
	savedSeed := newCrawlerSeedSource
	savedPublicWeb := newCrawlerPublicWebAdmissionFetcher
	t.Cleanup(func() {
		newCrawlerExchange = savedExchange
		newCrawlerRobotsAdmissionFetcher = savedRobots
		newCrawlerHTTPPageFetcher = savedHTTP
		newCrawlerSeedSource = savedSeed
		newCrawlerPublicWebAdmissionFetcher = savedPublicWeb
	})
}

func stubExchange(t *testing.T, exchange *fakeExchange) {
	t.Helper()
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return exchange, io.NopCloser(nil), nil
	}
}

// serveViaSlowSource points the fast fetcher at a real HTTP client (which the
// egress guard refuses for the loopback origin) and strips the public-web
// admission layer, so page content is served by the fallback `source` handed to
// RunService — letting a test drive deterministic page bodies in-process.
func serveViaSlowSource(t *testing.T) {
	t.Helper()
	newCrawlerHTTPPageFetcher = func(*http.Client, string, int64) *httpfetch.PageFetcher {
		return httpfetch.NewPageFetcher(http.DefaultClient, "", 0)
	}
	newCrawlerPublicWebAdmissionFetcher = func(
		inner pagefetch.PageSource,
		_ publicweb.Resolver,
		_ yagoegress.Guard,
	) pagefetch.PageSource {
		return inner
	}
}

func TestRunServiceDrivesOrdersToIngest(t *testing.T) {
	restoreAssemblySeams(t)
	newCrawlerHTTPPageFetcher = func(*http.Client, string, int64) *httpfetch.PageFetcher {
		return httpfetch.NewPageFetcher(http.DefaultClient, "", 0)
	}
	newCrawlerPublicWebAdmissionFetcher = func(
		inner pagefetch.PageSource,
		_ publicweb.Resolver,
		_ yagoegress.Guard,
	) pagefetch.PageSource {
		return inner
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fast client forbidden", http.StatusForbidden)
	}))
	defer origin.Close()

	exchange := &fakeExchange{
		orders:   []*crawlrpc.CrawlOrderMessage{orderMessage(t, origin.URL)},
		ingested: make(chan *crawlrpc.IngestBatchMessage, 1),
	}
	stubExchange(t, exchange)

	source := htmlPageSource(map[string]string{"/": "words here"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- RunService(ctx, serviceConfig(), source) }()

	select {
	case msg := <-exchange.ingested:
		batch, err := yagocrawlcontract.UnmarshalIngestBatch(msg.GetBatchJson())
		if err != nil {
			t.Fatalf("decode ingest: %v", err)
		}
		if string(batch.Provenance) != "admin" {
			t.Errorf("batch provenance = %q, want admin", batch.Provenance)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no ingest batch submitted")
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

func TestRunServiceReturnsDialError(t *testing.T) {
	restoreAssemblySeams(t)
	sentinel := errors.New("dial failed")
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return nil, nil, sentinel
	}

	err := RunService(context.Background(), serviceConfig(), htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "dial node rpc") {
		t.Fatalf("error = %v, want dial node rpc error", err)
	}
}

func TestRunServiceReturnsCrawlPaceError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	cfg := serviceConfig()
	cfg.Crawl.HostCacheSize = 0

	err := RunService(context.Background(), cfg, htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "create crawl pace") {
		t.Fatalf("error = %v, want create crawl pace error", err)
	}
}

func TestRunServiceReturnsRobotsAdmissionError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	sentinel := errors.New("robots failed")
	newCrawlerRobotsAdmissionFetcher = func(
		pagefetch.PageSource,
		*http.Client,
		string,
		int,
		...robots.Option,
	) (*robots.RobotsAdmissionFetcher, error) {
		return nil, sentinel
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := RunService(ctx, serviceConfig(), htmlPageSource(map[string]string{}))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestRunServiceReturnsMetricsBindError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	cfg := serviceConfig()
	cfg.MetricsAddr = "not-an-address"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := RunService(ctx, cfg, htmlPageSource(map[string]string{})); err == nil {
		t.Fatal("expected crawler metrics bind error")
	}
}

func TestDefaultPublicWebAdmissionFetcherBuildsFetcher(t *testing.T) {
	got := newCrawlerPublicWebAdmissionFetcher(
		htmlPageSource(map[string]string{}),
		nil,
		yagoegress.NewGuard(false),
	)
	if got == nil {
		t.Fatal("public web admission fetcher is nil")
	}
}

func TestDefaultHTTPPageFetcherBuildsFetcher(t *testing.T) {
	got := newCrawlerHTTPPageFetcher(http.DefaultClient, "agent/1.0", 1<<20)
	if got == nil {
		t.Fatal("http page fetcher is nil")
	}
}

func TestDefaultSeedSourceBuildsFetcher(t *testing.T) {
	got := newCrawlerSeedSource(http.DefaultClient, "agent/1.0", 1<<20)
	if got == nil {
		t.Fatal("seed source is nil")
	}
}

func TestNewCrawlerExchangeDialsWellFormedTarget(t *testing.T) {
	client, closer, err := newCrawlerExchange("localhost:9091")
	if err != nil {
		t.Fatalf("dial well-formed target: %v", err)
	}
	if client == nil || closer == nil {
		t.Fatal("client/closer nil for a well-formed target")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestNewCrawlerExchangeRejectsMalformedTarget(t *testing.T) {
	if _, _, err := newCrawlerExchange("\x00bad"); err == nil {
		t.Fatal("expected an error for a malformed target")
	}
}

func serviceConfig() ServiceConfig {
	getenv := func(key string) string {
		switch key {
		case EnvNodeRPCAddr:
			return "node.invalid:9091"
		case EnvWorkers:
			return "1"
		default:
			return ""
		}
	}
	cfg, err := LoadServiceConfig(getenv)
	if err != nil {
		panic(err)
	}

	return cfg
}

func orderMessage(t *testing.T, target string) *crawlrpc.CrawlOrderMessage {
	t.Helper()
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile: yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
			Name:            "default",
			Scope:           yagocrawlcontract.ScopeDomain,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxDepth:        0,
			MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		}),
	}
	order.Requests = []yagocrawlcontract.CrawlRequest{
		{URL: target, ProfileHandle: order.Profile.Handle},
	}
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}

	return &crawlrpc.CrawlOrderMessage{OrderJson: data}
}

func TestRunServiceReturnsInsecureRobotsAdmissionError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	sentinel := errors.New("insecure robots failed")
	calls := 0
	newCrawlerRobotsAdmissionFetcher = func(
		inner pagefetch.PageSource,
		client *http.Client,
		agent string,
		cacheSize int,
		opts ...robots.Option,
	) (*robots.RobotsAdmissionFetcher, error) {
		calls++
		// The verifying chain builds first; fail only the insecure one.
		if calls == 2 {
			return nil, sentinel
		}

		return robots.NewRobotsAdmissionFetcher(inner, client, agent, cacheSize, opts...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := RunService(ctx, serviceConfig(), htmlPageSource(map[string]string{}))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestRunServiceReturnsAdaptivePaceError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	saved := newCrawlerAdaptivePace
	t.Cleanup(func() { newCrawlerAdaptivePace = saved })
	sentinel := errors.New("adaptive pace failed")
	newCrawlerAdaptivePace = func(
		*crawldelay.HostPace,
		int,
		crawldelay.BackoffObserver,
	) (*crawldelay.AdaptivePace, error) {
		return nil, sentinel
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := RunService(ctx, serviceConfig(), htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "create adaptive crawl pace") {
		t.Fatalf("error = %v, want create adaptive crawl pace error", err)
	}
}
