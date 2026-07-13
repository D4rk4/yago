package crawlorder

import (
	"context"
	"log/slog"
)

func settleMalformedOrder(ctx context.Context, client OrderStreamer, leaseID string) {
	if err := settleLease(
		ctx,
		client,
		leaseID,
		false,
	)(context.WithoutCancel(ctx)); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderTermFailed,
			slog.String("leaseId", leaseID),
			slog.Any("error", err),
		)
	}
}
