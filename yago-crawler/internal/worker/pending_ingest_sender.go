package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (worker *Worker) runPendingIngest(ctx context.Context) {
	for ctx.Err() == nil {
		sessionContext, stopSession := context.WithCancel(ctx)
		stream, err := worker.exchange.StreamDocumentMutations(sessionContext)
		if err == nil {
			err = worker.runPendingIngestStream(sessionContext, stream)
		}
		stopSession()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.WarnContext(ctx, messageIngestFailed,
				slog.String("stage", "node_ingest_stream"),
				slog.Any("error", err))
		}
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

func (worker *Worker) runPendingIngestStream(
	ctx context.Context,
	stream interface {
		Send(*crawlrpc.DocumentMutation) error
		Recv() (*crawlrpc.DocumentMutationAck, error)
		CloseSend() error
	},
) error {
	defer stream.CloseSend()
	acknowledgments := make(chan *crawlrpc.DocumentMutationAck, yagocrawlcontract.MaximumIngestBatchRecords)
	receiveErrors := make(chan error, 1)
	go receiveIngestAcknowledgments(ctx, stream, acknowledgments, receiveErrors)
	inFlight := make(map[string]frontier.PendingIngest, yagocrawlcontract.MaximumIngestBatchRecords)
	sentAt := make(map[string]time.Time, yagocrawlcontract.MaximumIngestBatchRecords)
	defer func() {
		for _, pending := range inFlight {
			worker.clearPageActivity(pending.PageID)
			worker.releasePendingIngest(pending.PageID)
		}
	}()
	for ctx.Err() == nil {
		for len(inFlight) < yagocrawlcontract.MaximumIngestBatchRecords {
			pending, found, err := worker.claimPendingIngest(ctx)
			if err != nil {
				slog.WarnContext(ctx, messageIngestFailed, slog.Any("error", err))
				break
			}
			if !found {
				break
			}
			if _, err := yagocrawlcontract.UnmarshalIngestBatch(pending.BatchJSON); err != nil {
				slog.ErrorContext(ctx, messageIngestFailed,
					slog.String("pageId", pending.PageID),
					slog.String("host", hostnameFromPageURL(pending.URL)),
					slog.String("stage", "node_ingest_decode"),
					slog.Any("error", err))
				worker.schedulePendingIngestRetry(ctx, pending)
				worker.releasePendingIngest(pending.PageID)
				continue
			}
			message := &crawlrpc.DocumentMutation{
				MutationId: pending.PageID,
				BatchJson:  pending.BatchJSON,
			}
			worker.setPageActivity(pending.RunID, pending.PageID, pending.URL, "node_ingest")
			if err := stream.Send(message); err != nil {
				worker.activity.recordIngestSend(true)
				worker.clearPageActivity(pending.PageID)
				worker.releasePendingIngest(pending.PageID)
				return err
			}
			worker.activity.recordIngestSend(false)
			inFlight[pending.PageID] = pending
			sentAt[pending.PageID] = time.Now()
		}
		select {
		case acknowledgment := <-acknowledgments:
			pending, found := inFlight[acknowledgment.GetMutationId()]
			if !found {
				slog.WarnContext(ctx, messageIngestFailed,
					slog.String("stage", "node_ingest_ack"),
					slog.String("mutationId", acknowledgment.GetMutationId()))
				continue
			}
			delete(inFlight, pending.PageID)
			acknowledgedAt := sentAt[pending.PageID]
			delete(sentAt, pending.PageID)
			worker.activity.recordNodeAcknowledgment(
				time.Since(acknowledgedAt),
				acknowledgment.GetErrorCode(),
				acknowledgment.GetRejected(),
			)
			worker.completePendingIngestAcknowledgment(ctx, pending, acknowledgment)
			worker.releasePendingIngest(pending.PageID)
		case err := <-receiveErrors:
			return err
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func receiveIngestAcknowledgments(
	ctx context.Context,
	stream interface {
		Recv() (*crawlrpc.DocumentMutationAck, error)
	},
	acknowledgments chan<- *crawlrpc.DocumentMutationAck,
	receiveErrors chan<- error,
) {
	for {
		acknowledgment, err := stream.Recv()
		if err != nil {
			select {
			case receiveErrors <- err:
			case <-ctx.Done():
			}
			return
		}
		select {
		case acknowledgments <- acknowledgment:
		case <-ctx.Done():
			return
		}
	}
}

func (worker *Worker) completePendingIngestAcknowledgment(
	ctx context.Context,
	pending frontier.PendingIngest,
	acknowledgment *crawlrpc.DocumentMutationAck,
) {
	worker.clearPageActivity(pending.PageID)
	if acknowledgment.GetErrorCode() != 0 {
		slog.WarnContext(ctx, messageIngestFailed,
			slog.String("pageId", pending.PageID),
			slog.String("host", hostnameFromPageURL(pending.URL)),
			slog.String("stage", "node_ingest"),
			slog.Int("grpcCode", int(acknowledgment.GetErrorCode())))
		worker.schedulePendingIngestRetry(ctx, pending)
		return
	}
	if err := worker.frontier.CompletePendingIngestWithOutcome(
		ctx,
		pending.PageID,
		!acknowledgment.GetRejected(),
		acknowledgment.GetRejectionRule(),
	); err != nil {
		slog.ErrorContext(ctx, messageIngestFailed,
			slog.String("pageId", pending.PageID),
			slog.String("host", hostnameFromPageURL(pending.URL)),
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
			slog.String("pageId", pending.PageID),
			slog.String("host", hostnameFromPageURL(pending.URL)),
			slog.String("stage", "node_ingest_retry"),
			slog.Any("error", err))
	}
}
