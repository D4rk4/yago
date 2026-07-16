package crawlorder

import (
	"context"
	"log/slog"
)

type heartbeatDelivery struct {
	client        OrderStreamer
	workerID      string
	control       ControlHandler
	activeFetches func() uint32
}

func (d heartbeatDelivery) deliver(ctx context.Context) {
	result, err := d.client.Heartbeat(ctx, workerHeartbeat(d.workerID, d.activeFetches))
	if err != nil {
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return
	}
	dispatchDirectives(ctx, d.control, result.GetDirectives())
}
