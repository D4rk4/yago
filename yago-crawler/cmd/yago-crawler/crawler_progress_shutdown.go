package main

import (
	"context"
	"log/slog"
	"time"
)

const msgProgressShutdownElapsed = "crawl progress shutdown grace elapsed"

type crawlerProgressCloser interface {
	Close(context.Context) error
}

func closeCrawlerProgress(
	logCtx context.Context,
	progress crawlerProgressCloser,
	grace time.Duration,
) {
	progressCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := progress.Close(progressCtx); err != nil {
		slog.WarnContext(logCtx, msgProgressShutdownElapsed,
			slog.Duration("grace", grace),
			slog.Any("error", err))
	}
}
