package yacycrawler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func RunService(ctx context.Context, cfg ServiceConfig, source PageSource) error {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("init jetstream: %w", err)
	}
	if err := EnsureStreams(ctx, js, cfg.StreamSpec()); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	orders, err := NewNATSOrderReceiver(ctx, js, cfg.OrdersDurable, cfg.OrdersSubject)
	if err != nil {
		return fmt.Errorf("create order receiver: %w", err)
	}
	ingest := NewNATSIngestPublisher(js, cfg.IngestSubject)

	crawl := cfg.Crawl
	frontier := NewFrontier(crawl.JobQueueSize)

	client := &http.Client{Timeout: crawl.RequestTimeout}
	gate := NewPolitenessGate(client, crawl.UserAgent, crawl.CrawlDelay)
	polite := NewPolitePageFetcher(source, gate)
	publisher := NewIngestPublisher(ingest)
	botWall := NewBotWallDetector()
	pipeline := NewPipeline(frontier, polite, publisher, frontier, botWall)
	consumer := NewCrawlOrderConsumer(orders, frontier)

	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, crawl.Workers)
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
