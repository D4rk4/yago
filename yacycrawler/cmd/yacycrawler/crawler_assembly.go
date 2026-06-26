package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/botwall"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlorder"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/ingest"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pipeline"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/politeness"
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
	frontier := frontier.NewFrontier(crawl.JobQueueSize)

	client := &http.Client{Timeout: crawl.RequestTimeout}
	gate := politeness.NewPolitenessGate(client, crawl.UserAgent, crawl.CrawlDelay)
	polite := politeness.NewPolitePageFetcher(source, gate)
	worker := pipeline.NewPipeline(
		frontier,
		polite,
		botwall.NewBotWallDetector(),
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
