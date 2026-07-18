package yagonode

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/transfertally"
)

const (
	transferTallyFlushInterval = time.Second
	transferTallyDrainTimeout  = 5 * time.Second
)

type transferTallyFlusher interface {
	Flush(context.Context) error
}

func startTransferTallyFlush(
	ctx context.Context,
	background *sync.WaitGroup,
	tally *transfertally.Tally,
) {
	if tally == nil {
		return
	}
	background.Add(1)
	go func() {
		defer background.Done()
		ticker := time.NewTicker(transferTallyFlushInterval)
		defer ticker.Stop()
		runTransferTallyFlush(ctx, tally, ticker.C)
	}()
}

func runTransferTallyFlush(
	ctx context.Context,
	tally transferTallyFlusher,
	ticks <-chan time.Time,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			flushTransferTally(ctx, tally)
		}
	}
}

func awaitBackgroundAndDrainTransferTally(
	background *sync.WaitGroup,
	tally *transfertally.Tally,
) {
	background.Wait()
	if tally == nil {
		return
	}
	drainCtx, cancel := context.WithTimeout(
		context.Background(),
		transferTallyDrainTimeout,
	)
	flushTransferTally(drainCtx, tally)
	cancel()
}

func flushTransferTally(ctx context.Context, tally transferTallyFlusher) {
	if err := tally.Flush(ctx); err != nil {
		slog.WarnContext(ctx, msgTransferTallyFailed, slog.Any("error", err))
	}
}
