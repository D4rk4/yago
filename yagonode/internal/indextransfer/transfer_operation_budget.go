package indextransfer

import (
	"context"
	"net/http"
	"time"
)

func transferOperationContext(
	ctx context.Context,
	client *http.Client,
) (context.Context, context.CancelFunc) {
	if client.Timeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, client.Timeout)
}

func transferAttemptContext(
	ctx context.Context,
	remainingEndpoints int,
) (context.Context, context.CancelFunc) {
	deadline, bounded := ctx.Deadline()
	if !bounded || remainingEndpoints <= 1 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(
		ctx,
		time.Until(deadline)/time.Duration(remainingEndpoints),
	)
}
