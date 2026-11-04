package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	inboundAnchorRefreshInterval    = 10 * time.Second
	inboundAnchorRefreshPageSize    = 64
	inboundAnchorRefreshFailMessage = "inbound anchor refresh failed"
	inboundAnchorRefreshDoneMessage = "inbound anchor refresh completed"
)

var newInboundAnchorRefreshTicks = func() (<-chan time.Time, func()) {
	ticker := time.NewTicker(inboundAnchorRefreshInterval)

	return ticker.C, ticker.Stop
}

func inboundAnchorRefresher(storage nodeStorage) documentstore.InboundAnchorDocumentRefresher {
	refresher, _ := storage.documentReceiver.(documentstore.InboundAnchorDocumentRefresher)

	return refresher
}

func runInboundAnchorRefreshLoop(
	ctx context.Context,
	refresher documentstore.InboundAnchorDocumentRefresher,
	index documentUpdateIndexer,
) {
	if refresher == nil {
		return
	}
	ticks, stop := newInboundAnchorRefreshTicks()
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			refreshInboundAnchorsOnce(ctx, refresher, index)
		}
	}
}

func refreshInboundAnchorsOnce(
	ctx context.Context,
	refresher documentstore.InboundAnchorDocumentRefresher,
	index documentUpdateIndexer,
) {
	refreshed, err := drainInboundAnchorRefreshes(ctx, refresher, index)
	if err != nil {
		slog.WarnContext(
			ctx,
			inboundAnchorRefreshFailMessage,
			slog.Int("refreshed", refreshed),
			slog.Any("error", err),
		)

		return
	}
	slog.DebugContext(ctx, inboundAnchorRefreshDoneMessage, slog.Int("refreshed", refreshed))
}

func drainInboundAnchorRefreshes(
	ctx context.Context,
	refresher documentstore.InboundAnchorDocumentRefresher,
	index documentUpdateIndexer,
) (int, error) {
	reindex := func(documents []documentstore.Document) error {
		return indexAnchorDocuments(ctx, index, documents)
	}
	refreshed := 0
	for {
		receipt, err := refresher.RefreshInboundAnchorDocuments(
			ctx,
			inboundAnchorRefreshPageSize,
			reindex,
		)
		if err != nil {
			return refreshed, fmt.Errorf("refresh inbound anchor documents: %w", err)
		}
		refreshed += receipt.Refreshed
		if !receipt.More {
			return refreshed, nil
		}
	}
}

func indexAnchorDocuments(
	ctx context.Context,
	index documentUpdateIndexer,
	documents []documentstore.Document,
) error {
	if index == nil {
		return nil
	}
	if batch, ok := index.(documentBatchUpdateIndexer); ok {
		if err := batch.IndexBatch(ctx, documents); err != nil {
			return fmt.Errorf("index anchor document batch: %w", err)
		}

		return nil
	}
	for _, document := range documents {
		if err := index.Index(ctx, document); err != nil {
			return fmt.Errorf("index anchor document: %w", err)
		}
	}

	return nil
}
