package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/botwall"
	"github.com/D4rk4/yago/yacycrawler/internal/crawldelay"
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

var connectCrawlerNATS = nats.Connect

var newCrawlerJetStream = jetstream.New

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
	nc, err := connectCrawlerNATS(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	js, err := newCrawlerJetStream(nc)
	if err != nil {
		return fmt.Errorf("init jetstream: %w", err)
	}
	if err := yacycrawlcontract.EnsureStreams(ctx, js, cfg.StreamSpec()); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	orders, err := crawlorder.NewNATSOrderReceiver(ctx, js, cfg.OrdersDurable, cfg.OrdersSubject)
	if err != nil {
		return fmt.Errorf("create order receiver: %w", err)
	}
	emitter := ingest.NewBatchEmitter(ingest.NewNATSIngestPublisher(js, cfg.IngestSubject))

	crawl := cfg.Crawl
	pace, err := crawldelay.NewHostPace(crawl.CrawlDelay, crawl.HostCacheSize)
	if err != nil {
		return fmt.Errorf("create crawl pace: %w", err)
	}
	frontier := frontier.NewFrontier(crawl.JobQueueSize, pace)

	guard := yacyegress.NewGuard(cfg.EgressAllowLAN)
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
		slog.String("ordersSubject", cfg.OrdersSubject),
		slog.String("ingestSubject", cfg.IngestSubject),
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
