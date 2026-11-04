package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (worker *Worker) runPendingIngest(ctx context.Context) {
	jobs := make(chan frontier.PendingIngest, yagocrawlcontract.MaximumIngestBatchRecords)
	var senders sync.WaitGroup
	for range yagocrawlcontract.MaximumIngestBatchRecords {
		senders.Add(1)
		go func() {
			defer senders.Done()
			worker.runPendingIngestSender(ctx, jobs)
		}()
	}
	for ctx.Err() == nil {
		pending, found, err := worker.claimPendingIngest(ctx)
		if err != nil {
			slog.WarnContext(ctx, messageIngestFailed, slog.Any("error", err))
			if !waitContext(ctx, time.Second) {
				break
			}
			continue
		}
		if !found {
			if !waitContext(ctx, 100*time.Millisecond) {
				break
			}
			continue
		}
		select {
		case jobs <- pending:
		case <-ctx.Done():
			worker.releasePendingIngest(pending.PageID)
		}
	}
	close(jobs)
	senders.Wait()
}

func (worker *Worker) runPendingIngestSender(
	ctx context.Context,
	jobs <-chan frontier.PendingIngest,
) {
	for pending := range jobs {
		worker.sendPendingIngest(ctx, pending)
		worker.releasePendingIngest(pending.PageID)
	}
}

func (worker *Worker) sendPendingIngest(ctx context.Context, pending frontier.PendingIngest) {
	if !worker.hasLease(pending.LeaseID) {
		if err := worker.frontier.RetryPendingIngest(
			ctx,
			pending.PageID,
			time.Now().Add(time.Second),
			true,
		); err != nil {
			slog.ErrorContext(ctx, messageIngestFailed,
				slog.String("url", pending.URL),
				slog.String("stage", "node_ingest_lease"),
				slog.Any("error", err))
		}
		return
	}
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(pending.BatchJSON)
	if err != nil {
		slog.ErrorContext(ctx, messageIngestFailed,
			slog.String("url", pending.URL),
			slog.String("stage", "node_ingest"),
			slog.Any("error", err))
		worker.schedulePendingIngestRetry(ctx, pending)
		return
	}
	worker.setPageActivity(pending.PageID, pending.URL, "node_ingest")
	logPageStageStarted(ctx, pending.URL, "node_ingest")
	acknowledgment, err := worker.exchange.SubmitIngest(ctx, pending.LeaseID, batch)
	worker.clearPageActivity(pending.PageID)
	if err != nil {
		slog.WarnContext(ctx, messageIngestFailed,
			slog.String("url", pending.URL),
			slog.String("stage", "node_ingest"),
			slog.Duration("elapsed", time.Since(pending.NotBefore)),
			slog.Any("error", err))
		if ctx.Err() == nil {
			worker.schedulePendingIngestRetry(ctx, pending)
		}
		return
	}
	if err := worker.frontier.CompletePendingIngest(
		ctx,
		pending.PageID,
		!acknowledgment.GetRejected(),
	); err != nil {
		slog.ErrorContext(ctx, messageIngestFailed,
			slog.String("url", pending.URL),
			slog.String("stage", "node_ingest_ack"),
			slog.Any("error", err))
		worker.schedulePendingIngestRetry(ctx, pending)
	}
}

func (worker *Worker) schedulePendingIngestRetry(
	ctx context.Context,
	pending frontier.PendingIngest,
) {
	delay := 100 * time.Millisecond
	for range min(pending.Attempts, 6) {
		delay = min(delay*2, 5*time.Second)
	}
	if err := worker.frontier.RetryPendingIngest(
		ctx,
		pending.PageID,
		time.Now().Add(delay),
		false,
	); err != nil {
		slog.ErrorContext(ctx, messageIngestFailed,
			slog.String("url", pending.URL),
			slog.String("stage", "node_ingest_retry"),
			slog.Any("error", err))
	}
}
