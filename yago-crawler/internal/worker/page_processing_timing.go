package worker

import (
	"context"
	"log/slog"
	"time"
)

const (
	messagePageProcessingTiming = "crawler page processing timing"
)

func logPageProcessingTiming(
	ctx context.Context,
	pageID string,
	hostname string,
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
		slog.String("pageId", pageID),
		slog.String("host", hostname),
		slog.Duration("elapsed", elapsed),
		slog.Bool("failed", failed),
	)
}
