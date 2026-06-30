package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/botwall"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawldelay"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlorder"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/ingest"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pipeline"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/robots"
)

func RunService(ctx context.Context, cfg ServiceConfig, source pagefetch.PageSource) error {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
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

	client := newEgressProxyClient(cfg.ProxyURL, crawl.RequestTimeout)
	admitted, err := robots.NewRobotsAdmissionFetcher(
		source,
		client,
		crawl.UserAgent,
		crawl.HostCacheSize,
	)
	if err != nil {
		return fmt.Errorf("create robots admission: %w", err)
	}
	screened := botwall.NewBotWallScreeningFetcher(admitted)
	worker := pipeline.NewPipeline(
		frontier,
		screened,
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
