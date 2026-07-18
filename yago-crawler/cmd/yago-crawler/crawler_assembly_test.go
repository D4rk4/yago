package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldelay"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/publicweb"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagoegress"
)

type fakeExchange struct {
	orders        []*crawlrpc.CrawlOrderMessage
	ingested      chan *crawlrpc.IngestBatchMessage
	progress      chan *crawlrpc.CrawlProgressReport
	streamContext chan context.Context
	streamDone    chan struct{}
	streamErr     error
}

type crawlerRobotsRoundTrip func(*http.Request) (*http.Response, error)

type closeRecorder struct {
	closed chan struct{}
}

func (c closeRecorder) Close() error {
	close(c.closed)

	return nil
}

func (transport crawlerRobotsRoundTrip) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return transport(request)
}

func (f *fakeExchange) StreamOrders(
	ctx context.Context,
	_ *crawlrpc.WorkerRegistration,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	if f.streamContext != nil {
		f.streamContext <- ctx
	}

	return &fakeOrderClientStream{
		ctx: ctx, orders: f.orders, done: f.streamDone,
	}, nil
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
	ctx context.Context,
	request *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	result := &crawlrpc.OrderAckResult{}
	if len(request.GetOrderIdentity()) != 0 && len(request.GetConfirmationToken()) == 0 {
		result.ConfirmationToken = make([]byte, sha256.Size)
		if f.progress != nil {
			report := &crawlrpc.CrawlProgressReport{
				WorkerId:        request.GetWorkerId(),
				WorkerSessionId: request.GetWorkerSessionId(),
				State:           request.GetTerminalState(),
				Tally:           request.GetTerminalTally(),
				PagesPerMinute:  request.PagesPerMinute,
				LeaseId:         request.GetLeaseId(),
			}
			select {
			case f.progress <- report:
			case <-ctx.Done():
				return nil, fmt.Errorf("record crawler test progress: %w", ctx.Err())
			}
		}
	}

	return result, nil
}

func (f *fakeExchange) Heartbeat(
	_ context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      append([]string(nil), heartbeat.GetActiveLeaseIds()...),
		LeaseTtlMilliseconds: uint64((2 * time.Minute) / time.Millisecond),
	}, nil
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
	done   chan<- struct{}
}

func (s *fakeOrderClientStream) Recv() (*crawlrpc.CrawlOrderMessage, error) {
	if s.index < len(s.orders) {
		msg := s.orders[s.index]
		s.index++

		return msg, nil
	}
	<-s.ctx.Done()
	if s.done != nil {
		close(s.done)
	}

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

func TestFetchChainsLeaveBrowserFailureIsolationToThePool(t *testing.T) {
	restoreAssemblySeams(t)
	newCrawlerPublicWebAdmissionFetcher = func(
		inner pagefetch.PageSource,
		_ publicweb.Resolver,
		_ yagoegress.Guard,
	) pagefetch.PageSource {
		return inner
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "retry through browser", http.StatusForbidden)
	}))
	defer origin.Close()

	crawl := DefaultCrawlConfig()
	crawl.BrowserFailureThreshold = 2
	calls := 0
	slow := pageSourceFunc(func(
		_ context.Context,
		target *url.URL,
	) (pagefetch.FetchedPage, error) {
		calls++
		if calls <= crawl.BrowserFailureThreshold {
			return pagefetch.FetchedPage{}, errors.New("browser slot failed")
		}

		return pagefetch.FetchedPage{
			URL:         target,
			ContentType: "text/html",
			Body:        []byte("<html><body>browser pool recovered</body></html>"),
		}, nil
	})
	chains, err := buildFetchChains(
		yagoegress.NewGuard(true),
		origin.Client(),
		crawl,
		slow,
		crawlermetrics.New(),
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	for range crawl.BrowserFailureThreshold {
		if _, err := chains.verifyingDirect.Fetch(t.Context(), target); err == nil {
			t.Fatal("browser slot failure was not returned")
		}
	}
	page, err := chains.verifyingDirect.Fetch(t.Context(), target)
	if err != nil {
		t.Fatalf("browser pool recovery: %v", err)
	}
	if calls != crawl.BrowserFailureThreshold+1 || len(page.Body) == 0 {
		t.Fatalf("browser calls/body = %d/%q", calls, page.Body)
	}
}

// serveViaSlowSource points the fast fetcher at a real HTTP client (which the
// egress guard refuses for the loopback origin) and strips the public-web
// admission layer, so page content is served by the fallback `source` handed to
// RunService — letting a test drive deterministic page bodies in-process.
func serveViaSlowSource(t *testing.T) {
	t.Helper()
	newCrawlerRobotsAdmissionFetcher = func(
		inner pagefetch.PageSource,
		_ *http.Client,
		userAgent string,
		hostCacheSize int,
		options ...robots.Option,
	) (*robots.RobotsAdmissionFetcher, error) {
		client := &http.Client{Transport: crawlerRobotsRoundTrip(
			func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						"User-agent: *\nAllow: /\n",
					)),
					Request: request,
				}, nil
			},
		)}

		return robots.NewRobotsAdmissionFetcher(
			inner,
			client,
			userAgent,
			hostCacheSize,
			options...,
		)
	}
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
	serveViaSlowSource(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fast client forbidden", http.StatusForbidden)
	}))
	defer origin.Close()

	controlExchange := &fakeExchange{
		orders:   []*crawlrpc.CrawlOrderMessage{orderMessage(t, origin.URL)},
		progress: make(chan *crawlrpc.CrawlProgressReport, 4),
	}
	ingestExchange := &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)}
	dials := 0
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		dials++
		if dials == 1 {
			return controlExchange, io.NopCloser(nil), nil
		}

		return ingestExchange, io.NopCloser(nil), nil
	}

	source := htmlPageSource(map[string]string{"/": "words here"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- RunService(ctx, serviceConfig(t), source) }()

	select {
	case msg := <-ingestExchange.ingested:
		if msg.GetLeaseId() != "assembly-lease" || msg.GetWorkerId() == "" ||
			msg.GetWorkerSessionId() == "" {
			t.Fatalf("ingest lease identity = %+v", msg)
		}
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
	if dials != 2 {
		t.Fatalf("crawl exchange dials = %d, want separate control and ingest connections", dials)
	}
}

func TestRunServiceReturnsDialError(t *testing.T) {
	restoreAssemblySeams(t)
	sentinel := errors.New("dial failed")
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		return nil, nil, sentinel
	}

	err := RunService(context.Background(), serviceConfig(t), htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "dial node control rpc") {
		t.Fatalf("error = %v, want dial node control rpc error", err)
	}
}

func TestRunServiceReturnsCheckpointOpenError(t *testing.T) {
	cfg := serviceConfig(t)
	parent := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write checkpoint parent: %v", err)
	}
	cfg.DataDir = parent
	err := RunService(context.Background(), cfg, htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "open crawler frontier checkpoint") {
		t.Fatalf("error = %v, want checkpoint open error", err)
	}
}

func TestRunServiceReturnsWorkerIdentityError(t *testing.T) {
	cfg := serviceConfig(t)
	cfg.WorkerID = " "
	err := RunService(context.Background(), cfg, htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "load crawler worker identity") {
		t.Fatalf("error = %v, want worker identity error", err)
	}
}

func TestRunServiceClosesControlConnectionWhenIngestDialFails(t *testing.T) {
	restoreAssemblySeams(t)
	sentinel := errors.New("ingest dial failed")
	closed := make(chan struct{})
	calls := 0
	newCrawlerExchange = func(string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		calls++
		if calls == 1 {
			return &fakeExchange{}, closeRecorder{closed: closed}, nil
		}

		return nil, nil, sentinel
	}

	err := RunService(context.Background(), serviceConfig(t), htmlPageSource(map[string]string{}))
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "dial node ingest rpc") {
		t.Fatalf("error = %v, want ingest dial error", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("control connection was not closed")
	}
}

func TestRunServiceReturnsCrawlPaceError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	cfg := serviceConfig(t)
	cfg.Crawl.HostCacheSize = 0

	err := RunService(context.Background(), cfg, htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "create crawl pace") {
		t.Fatalf("error = %v, want create crawl pace error", err)
	}
}

func TestRunServiceReturnsRobotsAdmissionError(t *testing.T) {
	restoreAssemblySeams(t)
	exchange := &fakeExchange{
		ingested:      make(chan *crawlrpc.IngestBatchMessage, 1),
		streamContext: make(chan context.Context, 1),
		streamDone:    make(chan struct{}),
	}
	stubExchange(t, exchange)
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

	err := RunService(ctx, serviceConfig(t), htmlPageSource(map[string]string{}))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	select {
	case receiverCtx := <-exchange.streamContext:
		select {
		case <-receiverCtx.Done():
		default:
			t.Fatal("order receiver context remained active after assembly failure")
		}
	case <-time.After(time.Second):
		t.Fatal("order stream did not start before assembly failure")
	}
	select {
	case <-exchange.streamDone:
	case <-time.After(time.Second):
		t.Fatal("order stream did not stop after assembly failure")
	}
}

func TestRunServiceReturnsMetricsBindError(t *testing.T) {
	restoreAssemblySeams(t)
	stubExchange(t, &fakeExchange{ingested: make(chan *crawlrpc.IngestBatchMessage, 1)})
	cfg := serviceConfig(t)
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

func serviceConfig(t *testing.T) ServiceConfig {
	t.Helper()
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
	cfg.DataDir = t.TempDir()

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

	return &crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: "assembly-lease"}
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

	err := RunService(ctx, serviceConfig(t), htmlPageSource(map[string]string{}))
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

	err := RunService(ctx, serviceConfig(t), htmlPageSource(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "create adaptive crawl pace") {
		t.Fatalf("error = %v, want create adaptive crawl pace error", err)
	}
}
