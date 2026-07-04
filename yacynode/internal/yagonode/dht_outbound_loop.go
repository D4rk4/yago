package yagonode

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
)

const (
	dhtOutboundCycleFailedMessage = "dht outbound cycle failed"
	dhtOutboundCycleSentMessage   = "dht outbound cycle sent"
)

type dhtOutboundCycle interface {
	RunOnce(context.Context) (dhtexchange.ScheduledDistributionReceipt, error)
}

type dhtOutboundProcess struct {
	cycle      dhtOutboundCycle
	interval   time.Duration
	gates      http.Handler
	gateStatus dhtGateStatusSource
}

var newDHTOutboundTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runDHTOutboundLoop(ctx context.Context, process dhtOutboundProcess) {
	runDHTOutboundOnce(ctx, process.cycle)

	ticks, stop := newDHTOutboundTicks(process.interval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			runDHTOutboundOnce(ctx, process.cycle)
		}
	}
}

func runDHTOutboundOnce(ctx context.Context, cycle dhtOutboundCycle) {
	receipt, err := cycle.RunOnce(ctx)
	if err != nil {
		slog.WarnContext(
			ctx,
			dhtOutboundCycleFailedMessage,
			slog.Any("error", err),
		)

		return
	}
	if receipt.Distribution.State != dhtexchange.DistributionSent {
		return
	}

	slog.DebugContext(
		ctx,
		dhtOutboundCycleSentMessage,
		slog.String("peer", receipt.Distribution.Peer.String()),
		slog.Int("postings", receipt.Distribution.PostingCount),
	)
}
