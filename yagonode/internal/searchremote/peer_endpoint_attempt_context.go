package searchremote

import (
	"context"
	"time"
)

func remoteSearchEndpointAttemptContext(
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
