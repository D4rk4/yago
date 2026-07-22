package peerannouncement

import (
	"context"
	"time"
)

func (g httpPeerGreeter) operationContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if g.client.Timeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, g.client.Timeout)
}

func greetAttemptContext(
	ctx context.Context,
	remainingEndpoints int,
) (context.Context, context.CancelFunc) {
	deadline, bounded := ctx.Deadline()
	if !bounded || remainingEndpoints <= 1 {
		return context.WithCancel(ctx)
	}
	remaining := time.Until(deadline)

	return context.WithTimeout(ctx, remaining/time.Duration(remainingEndpoints))
}
