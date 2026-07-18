package crawlorder

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
)

func settleMalformedOrderForSession(
	ctx context.Context,
	client OrderStreamer,
	acknowledgment leasedOrderAcknowledgment,
	leaseGrants *crawllease.GrantRegistry,
) error {
	settle := settleLeaseForSession(
		ctx,
		client,
		acknowledgment,
	)
	if leaseGrants != nil {
		settle = settleGrantedLease(settle, acknowledgment.leaseID, leaseGrants)
	}
	if err := settle(context.WithoutCancel(ctx)); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderTermFailed,
			slog.String("leaseId", acknowledgment.leaseID),
			slog.Any("error", err),
		)

		return err
	}

	return nil
}
