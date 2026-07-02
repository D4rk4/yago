package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/botwall"
	"github.com/D4rk4/yago/yacycrawler/internal/crawldelay"
	"github.com/D4rk4/yago/yacycrawler/internal/crawlorder"
	"github.com/D4rk4/yago/yacycrawler/internal/frontier"
	"github.com/D4rk4/yago/yacycrawler/internal/httpfetch"
	"github.com/D4rk4/yago/yacycrawler/internal/ingest"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
	"github.com/D4rk4/yago/yacycrawler/internal/pipeline"
	"github.com/D4rk4/yago/yacycrawler/internal/publicweb"
	"github.com/D4rk4/yago/yacycrawler/internal/robots"
)

var connectCrawlerNATS = nats.Connect

var newCrawlerJetStream = jetstream.New

var newCrawlerRobotsAdmissionFetcher = robots.NewRobotsAdmissionFetcher

var newCrawlerHTTPPageFetcher = httpfetch.NewPageFetcher

var newCrawlerPublicWebAdmissionFetcher = func(
	inner pagefetch.PageSource,
	resolver publicweb.Resolver,
) pagefetch.PageSource {
	return publicweb.NewAdmissionFetcher(inner, resolver)
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

	client := newEgressProxyClient(cfg.ProxyURL, crawl.RequestTimeout, crawl.MaxRedirects)
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
	publicOnly := newCrawlerPublicWebAdmissionFetcher(admitted, nil)
	worker := pipeline.NewPipeline(
		frontier,
		publicOnly,
		pageindex.NewIndexBuilder(),
		emitter,
	)
	consumer := crawlorder.NewCrawlOrderConsumer(orders, frontier)

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
