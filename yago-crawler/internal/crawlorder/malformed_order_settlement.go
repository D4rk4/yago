package crawlorder

import (
	"context"
	"log/slog"
)

func settleMalformedOrderForSession(
	ctx context.Context,
	client OrderStreamer,
	leaseID string,
	workerID string,
	workerSessionID string,
) {
	if err := settleLeaseForSession(
		ctx,
		client,
		leasedOrderAcknowledgment{
			leaseID:         leaseID,
			workerID:        workerID,
			workerSessionID: workerSessionID,
		},
	)(context.WithoutCancel(ctx)); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderTermFailed,
			slog.String("leaseId", leaseID),
			slog.Any("error", err),
		)
	}
}
