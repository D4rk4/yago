package crawlorder

import (
	"context"
	"time"
)

const DefaultHeartbeatRequestTimeout = time.Second

// orderHeartbeatRequestTimeout is the package default. Tests shrink it, so it
// must never be read from a background goroutine: the receiver's heartbeat loop
// outlives the test function that restores it, and reading it there raced with
// that restore. The receiver snapshots it once, on the constructing goroutine.
var orderHeartbeatRequestTimeout = DefaultHeartbeatRequestTimeout

// boundedHeartbeatContext bounds one heartbeat request. A delivery carrying no
// snapshot falls back to the package value, which is safe because every such
// caller runs the exchange synchronously rather than from a receiver goroutine.
func (d heartbeatDelivery) boundedHeartbeatContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	timeout := d.requestTimeout
	if timeout <= 0 {
		timeout = orderHeartbeatRequestTimeout
	}

	return context.WithTimeout(ctx, timeout)
}
