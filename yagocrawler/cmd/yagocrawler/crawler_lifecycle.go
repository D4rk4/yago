package main

import (
	"context"
	"log/slog"
)

type crawlerLifecycle struct {
	worker      crawlWorker
	consumer    orderConsumer
	progress    crawlerProgressCloser
	concurrency *workerConcurrency
}

func runCrawlerLifecycle(
	ctx context.Context,
	lifecycle crawlerLifecycle,
	config ServiceConfig,
) {
	slog.InfoContext(ctx, "crawler started",
		slog.String("nodeRpcAddr", config.NodeRPCAddr),
		slog.String("workerId", config.WorkerID),
		slog.Int("workers", config.Crawl.Workers),
	)
	superviseCrawlWithConcurrency(
		ctx,
		lifecycle.worker,
		lifecycle.consumer,
		lifecycle.concurrency,
		config.ShutdownGrace,
	)
	closeCrawlerProgress(ctx, lifecycle.progress, config.ShutdownGrace)
	slog.InfoContext(ctx, "crawler stopped")
}
