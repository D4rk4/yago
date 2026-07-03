package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yacycrawler/internal/botwall"
	"github.com/D4rk4/yago/yacycrawler/internal/crawldelay"
	"github.com/D4rk4/yago/yacycrawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yacycrawler/internal/crawlorder"
	"github.com/D4rk4/yago/yacycrawler/internal/crawlseed"
	"github.com/D4rk4/yago/yacycrawler/internal/frontier"
	"github.com/D4rk4/yago/yacycrawler/internal/httpfetch"
	"github.com/D4rk4/yago/yacycrawler/internal/ingest"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
	"github.com/D4rk4/yago/yacycrawler/internal/pipeline"
	"github.com/D4rk4/yago/yacycrawler/internal/publicweb"
	"github.com/D4rk4/yago/yacycrawler/internal/robots"
	"github.com/D4rk4/yago/yacyegress"
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
	guard yacyegress.Guard,
) pagefetch.PageSource {
	return publicweb.NewAdmissionFetcher(inner, resolver, guard)
}

func RunService(ctx context.Context, cfg ServiceConfig, source pagefetch.PageSource) error {
	exchange, closer, err := newCrawlerExchange(cfg.NodeRPCAddr)
	if err != nil {
		return fmt.Errorf("dial node rpc: %w", err)
	}
	defer func() { _ = closer.Close() }()

	orders := crawlorder.NewGRPCOrderReceiver(ctx, exchange, cfg.WorkerID)
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
	frontier := frontier.NewFrontier(crawl.JobQueueSize, pace)

	guard := yacyegress.NewGuard(
		cfg.EgressAllowLAN,
		yacyegress.WithPrivateAllowlist(cfg.EgressAllowedCIDRs),
	)
	client := newGuardedEgressClient(guard, crawl)
	fastSource := botwall.NewBotWallScreeningFetcher(
		newCrawlerHTTPPageFetcher(client, crawl.UserAgent, crawl.MaxBodyBytes),
	)
	slowSource := botwall.NewBotWallScreeningFetcher(source)
	selectedSource := pagefetch.NewFallbackPageSource(fastSource, slowSource)

	admitted, err := newCrawlerRobotsAdmissionFetcher(
		selectedSource,
		client,
		crawl.UserAgent,
		crawl.HostCacheSize,
		robots.WithDenialObserver(metrics),
	)
	if err != nil {
		return fmt.Errorf("create robots admission: %w", err)
	}
	publicOnly := newCrawlerPublicWebAdmissionFetcher(admitted, nil, guard)
	worker := pipeline.NewPipeline(
		frontier,
		publicOnly,
		pageindex.NewIndexBuilder(),
		emitter,
		pipeline.WithObserver(metrics),
	)
	consumer := crawlorder.NewCrawlOrderConsumer(
		orders,
		frontier,
		newCrawlRequestExpander(client, crawl, guard),
	)

	workersDone := make(chan struct{})
	go func() {
		worker.RunWorkers(ctx, crawl.Workers)
		close(workersDone)
	}()

	consumerDone := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(consumerDone)
	}()

	slog.InfoContext(ctx, "crawler started",
		slog.String("nodeRpcAddr", cfg.NodeRPCAddr),
		slog.String("workerId", cfg.WorkerID),
		slog.Int("workers", crawl.Workers),
	)
	<-consumerDone
	<-workersDone
	slog.InfoContext(ctx, "crawler stopped")
	return nil
}

func newCrawlRequestExpander(
	client *http.Client,
	crawl CrawlConfig,
	guard yacyegress.Guard,
) *crawlseed.Expander {
	seedSource := newCrawlerPublicWebAdmissionFetcher(
		newCrawlerSeedSource(client, crawl.UserAgent, crawl.MaxBodyBytes),
		nil,
		guard,
	)
	return crawlseed.NewExpander(seedSource, crawl.SitemapURLLimit)
}
