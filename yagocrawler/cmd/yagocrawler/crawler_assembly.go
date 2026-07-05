package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagocrawler/internal/botwall"
	"github.com/D4rk4/yago/yagocrawler/internal/crawldelay"
	"github.com/D4rk4/yago/yagocrawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yagocrawler/internal/crawlorder"
	"github.com/D4rk4/yago/yagocrawler/internal/crawlseed"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawler/internal/httpfetch"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawler/internal/publicweb"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
	"github.com/D4rk4/yago/yagocrawler/internal/runtally"
	"github.com/D4rk4/yago/yagoegress"
)

var newCrawlerExchange = func(addr string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("new crawl exchange client: %w", err)
	}

	return crawlrpc.NewCrawlExchangeClient(conn), conn, nil
}

var newCrawlerRobotsAdmissionFetcher = robots.NewRobotsAdmissionFetcher

var newCrawlerHTTPPageFetcher = httpfetch.NewPageFetcher

var newCrawlerSeedSource = crawlseed.NewHTTPSource

var newCrawlerPublicWebAdmissionFetcher = func(
	inner pagefetch.PageSource,
	resolver publicweb.Resolver,
	guard yagoegress.Guard,
) pagefetch.PageSource {
	return publicweb.NewAdmissionFetcher(inner, resolver, guard)
}

// fetchChains carries the two assembled page-fetch chains: the verifying
// default and the parallel one for profiles that opted into
// IgnoreTLSAuthority. Both share the browser fallback and the botwall,
// robots, and public-web layers; only the TLS client differs.
type fetchChains struct {
	verifying pagefetch.PageSource
	insecure  pagefetch.PageSource
}

func buildFetchChains(
	guard yagoegress.Guard,
	client *http.Client,
	crawl CrawlConfig,
	source pagefetch.PageSource,
	metrics *crawlermetrics.Metrics,
) (fetchChains, error) {
	slowSource := botwall.NewBotWallScreeningFetcher(source)
	fastSource := botwall.NewBotWallScreeningFetcher(
		newCrawlerHTTPPageFetcher(client, crawl.UserAgent, crawl.MaxBodyBytes),
	)
	admitted, err := newCrawlerRobotsAdmissionFetcher(
		pagefetch.NewFallbackPageSource(fastSource, slowSource),
		client,
		crawl.UserAgent,
		crawl.HostCacheSize,
		robots.WithDenialObserver(metrics),
	)
	if err != nil {
		return fetchChains{}, fmt.Errorf("create robots admission: %w", err)
	}

	insecureClient := newInsecureEgressClient(guard, crawl)
	insecureFast := botwall.NewBotWallScreeningFetcher(
		newCrawlerHTTPPageFetcher(insecureClient, crawl.UserAgent, crawl.MaxBodyBytes),
	)
	insecureAdmitted, err := newCrawlerRobotsAdmissionFetcher(
		pagefetch.NewFallbackPageSource(insecureFast, slowSource),
		insecureClient,
		crawl.UserAgent,
		crawl.HostCacheSize,
		robots.WithDenialObserver(metrics),
	)
	if err != nil {
		return fetchChains{}, fmt.Errorf("create insecure robots admission: %w", err)
	}

	return fetchChains{
		verifying: newCrawlerPublicWebAdmissionFetcher(admitted, nil, guard),
		insecure:  newCrawlerPublicWebAdmissionFetcher(insecureAdmitted, nil, guard),
	}, nil
}

func RunService(ctx context.Context, cfg ServiceConfig, source pagefetch.PageSource) error {
	exchange, closer, err := newCrawlerExchange(cfg.NodeRPCAddr)
	if err != nil {
		return fmt.Errorf("dial node rpc: %w", err)
	}
	defer func() { _ = closer.Close() }()

	emitter := ingest.NewBatchEmitter(ingest.NewGRPCIngestPublisher(exchange))

	metrics := crawlermetrics.New()
	metricsCloser, err := startCrawlerMetrics(ctx, cfg.MetricsAddr, metrics.Handler())
	if err != nil {
		return fmt.Errorf("start crawler metrics: %w", err)
	}
	defer func() { _ = metricsCloser.Close() }()

	crawl := cfg.Crawl
	pace, err := crawldelay.NewHostPace(crawl.CrawlDelay, crawl.HostCacheSize)
	if err != nil {
		return fmt.Errorf("create crawl pace: %w", err)
	}
	tally := runtally.New()
	frontier := frontier.NewFrontier(
		crawl.JobQueueSize,
		pace,
		frontier.WithMaxHostConcurrency(crawl.MaxHostConcurrency),
		frontier.WithRunTally(tally),
	)
	orders := crawlorder.NewGRPCOrderReceiver(
		ctx,
		exchange,
		cfg.WorkerID,
		crawlorder.NewFrontierControlHandler(frontier),
	)

	guard := yagoegress.NewGuard(
		cfg.EgressAllowLAN,
		yagoegress.WithPrivateAllowlist(cfg.EgressAllowedCIDRs),
	)
	client := newGuardedEgressClient(guard, crawl)
	chains, err := buildFetchChains(guard, client, crawl, source, metrics)
	if err != nil {
		return err
	}

	worker := pipeline.NewPipeline(
		frontier,
		chains.verifying,
		pageindex.NewIndexBuilder(),
		emitter,
		pipeline.WithObserver(metrics),
		pipeline.WithRunTally(tally),
		pipeline.WithInsecureFetcher(chains.insecure),
	)
	consumer := crawlorder.NewCrawlOrderConsumer(
		orders,
		frontier,
		newCrawlRequestExpander(client, crawl, guard),
	).WithProgressReporter(crawlorder.NewGRPCProgressReporter(exchange, cfg.WorkerID)).
		WithRunTally(tally)

	slog.InfoContext(ctx, "crawler started",
		slog.String("nodeRpcAddr", cfg.NodeRPCAddr),
		slog.String("workerId", cfg.WorkerID),
		slog.Int("workers", crawl.Workers),
	)
	superviseCrawl(ctx, worker, consumer, crawl.Workers, cfg.ShutdownGrace)
	slog.InfoContext(ctx, "crawler stopped")

	return nil
}

func newCrawlRequestExpander(
	client *http.Client,
	crawl CrawlConfig,
	guard yagoegress.Guard,
) *crawlseed.Expander {
	seedSource := newCrawlerPublicWebAdmissionFetcher(
		newCrawlerSeedSource(client, crawl.UserAgent, crawl.MaxBodyBytes),
		nil,
		guard,
	)
	return crawlseed.NewExpander(seedSource, crawl.SitemapURLLimit)
}
