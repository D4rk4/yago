package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func main() {
	os.Exit(start())
}

func start() int {
	cfg, err := yacycrawler.LoadServiceConfig(os.Getenv)
	if err != nil {
		slog.Error("crawler config invalid", "error", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.Error("crawler failed", "error", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg yacycrawler.ServiceConfig) error {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("init jetstream: %w", err)
	}
	if err := yacycrawler.EnsureStreams(ctx, js, cfg.StreamSpec()); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	orders, err := yacycrawler.NewNATSOrderReceiver(ctx, js, cfg.OrdersDurable, cfg.OrdersSubject)
	if err != nil {
		return fmt.Errorf("create order receiver: %w", err)
	}
	ingest := yacycrawler.NewNATSIngestPublisher(js, cfg.IngestSubject)

	crawl := cfg.Crawl
	jobs := yacycrawler.NewJobQueue(crawl.JobQueueSize)
	registry := yacycrawler.NewCrawlProfileRegistry()

	client := &http.Client{Timeout: crawl.RequestTimeout}
	fetcher, closeBrowser := yacycrawler.NewBrowserPageFetcher(
		crawl.UserAgent,
		crawl.RequestTimeout,
	)
	defer closeBrowser()
	gate := yacycrawler.NewPolitenessGate(client, crawl.UserAgent, crawl.CrawlDelay)
	polite := yacycrawler.NewPolitePageFetcher(fetcher, gate)
	publisher := yacycrawler.NewIngestPublisher(ingest)
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	botWall := yacycrawler.NewBotWallDetector()
	pipeline := yacycrawler.NewPipeline(jobs, polite, publisher, frontier, botWall)
	consumer := yacycrawler.NewCrawlOrderConsumer(orders, registry, frontier)

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

	slog.Info("crawler started",
		"orders_subject", cfg.OrdersSubject,
		"ingest_subject", cfg.IngestSubject,
		"workers", crawl.Workers,
	)
	<-consumerDone
	<-workersDone
	slog.Info("crawler stopped")
	return nil
}
