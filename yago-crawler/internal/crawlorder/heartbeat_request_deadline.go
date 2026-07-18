package crawlorder

import (
	"context"
	"time"
)

const DefaultHeartbeatRequestTimeout = time.Second

var orderHeartbeatRequestTimeout = DefaultHeartbeatRequestTimeout

func boundedHeartbeatContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, orderHeartbeatRequestTimeout)
}
