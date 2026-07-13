package main

import (
	"context"
	"log/slog"
)

func runCrawlerLifecycle(
	ctx context.Context,
	worker crawlWorker,
	consumer orderConsumer,
	progress crawlerProgressCloser,
	config ServiceConfig,
) {
	slog.InfoContext(ctx, "crawler started",
		slog.String("nodeRpcAddr", config.NodeRPCAddr),
		slog.String("workerId", config.WorkerID),
		slog.Int("workers", config.Crawl.Workers),
	)
	superviseCrawl(ctx, worker, consumer, config.Crawl.Workers, config.ShutdownGrace)
	closeCrawlerProgress(ctx, progress, config.ShutdownGrace)
	slog.InfoContext(ctx, "crawler stopped")
}
