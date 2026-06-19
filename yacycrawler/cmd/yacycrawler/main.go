package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func main() {
	workers := flag.Int("workers", yacycrawler.DefaultConfig().Workers, "number of crawl workers")
	depth := flag.Int("depth", yacycrawler.DefaultConfig().MaxDepth, "maximum link-following depth")
	flag.Parse()

	seeds := flag.Args()
	if len(seeds) == 0 {
		slog.Error("no seed urls given", "usage", "yacycrawler [-workers N] url...")
		os.Exit(2)
	}

	cfg := yacycrawler.DefaultConfig()
	cfg.SeedURLs = seeds
	cfg.Workers = *workers
	cfg.MaxDepth = *depth

	if err := run(cfg); err != nil {
		slog.Error("crawler failed", "error", err)
		os.Exit(1)
	}
}

func run(cfg yacycrawler.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	jobs := yacycrawler.NewJobQueue(cfg.JobQueueSize)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](cfg.IngestQueueSize)
	orders := yacycrawler.NewBoundedQueue[yacycrawlcontract.CrawlOrder](cfg.JobQueueSize)
	registry := yacycrawler.NewCrawlProfileRegistry()

	client := &http.Client{Timeout: cfg.RequestTimeout}
	fetcher, closeBrowser := yacycrawler.NewBrowserPageFetcher(cfg.UserAgent, cfg.RequestTimeout)
	defer closeBrowser()
	gate := yacycrawler.NewPolitenessGate(client, cfg.UserAgent, cfg.CrawlDelay)
	polite := yacycrawler.NewPolitePageFetcher(fetcher, gate)
	publisher := yacycrawler.NewIngestPublisher(ingest)
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, registry)
	botWall := yacycrawler.NewBotWallDetector()
	pipeline := yacycrawler.NewPipeline(jobs, polite, publisher, frontier, botWall)
	consumer := yacycrawler.NewCrawlOrderConsumer(orders, registry, frontier)
	node := yacycrawler.NewFakeNodeIngest(ingest)

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()

	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, cfg.Workers)
		close(workersDone)
	}()

	consumerDone := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(consumerDone)
	}()

	if err := orders.Publish(ctx, cfg.DefaultOrder([]byte("admin"))); err != nil {
		return fmt.Errorf("publish crawl order: %w", err)
	}
	orders.Close()
	<-consumerDone
	<-workersDone
	ingest.Close()
	<-nodeDone

	reportBatches(node.Batches())
	return nil
}

func reportBatches(batches []yacycrawler.IngestBatch) {
	postings := 0
	metadata := 0
	for _, batch := range batches {
		postings += len(batch.Postings)
		metadata += len(batch.Metadata)
	}
	slog.Info("crawl complete",
		"batches", len(batches),
		"postings", postings,
		"metadata", metadata,
	)
}
