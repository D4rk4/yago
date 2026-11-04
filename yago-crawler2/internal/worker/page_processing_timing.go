package worker

import (
	"context"
	"log/slog"
	"time"
)

const (
	messagePageProcessingTiming = "crawler page processing timing"
	messagePageStageStarted     = "crawler page stage started"
)

func logPageStageStarted(ctx context.Context, pageURL string, stage string) {
	slog.InfoContext(ctx, messagePageStageStarted,
		slog.String("url", pageURL),
		slog.String("stage", stage))
}

func logPageProcessingTiming(
	ctx context.Context,
	pageURL string,
	stage string,
	elapsed time.Duration,
	failed bool,
) {
	if elapsed < slowCrawlerOperationThreshold && !failed {
		return
	}
	slog.InfoContext(
		ctx,
		messagePageProcessingTiming,
		slog.String("stage", stage),
		slog.String("url", pageURL),
		slog.Duration("elapsed", elapsed),
		slog.Bool("failed", failed),
	)
}
