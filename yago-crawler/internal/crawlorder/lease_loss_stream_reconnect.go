package crawlorder

import "context"

func orderStreamAttemptContext(
	ctx context.Context,
	heartbeat *heartbeatDelivery,
) (context.Context, func()) {
	streamCtx, cancelStream := context.WithCancel(ctx)
	if heartbeat == nil || heartbeat.leaseGrants == nil {
		return streamCtx, cancelStream
	}
	losses := heartbeat.leaseGrants.LeaseLosses()
	finished := make(chan struct{})
	go func() {
		select {
		case <-losses:
			cancelStream()
		case <-finished:
		case <-ctx.Done():
		}
	}()

	return streamCtx, func() {
		close(finished)
		cancelStream()
	}
}
