package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

const (
	dhtOutboundPeerConfirmedMessage   = "dht outbound peer confirmed"
	dhtOutboundHandoffRejectedMessage = "dht outbound handoff rejected"
	dhtOutboundPeerQuarantinedMessage = "dht outbound peer quarantined"
)

type dhtOutboundPeerRoster interface {
	ConfirmReachable(context.Context, yagomodel.Hash)
	ConfirmUnreachable(context.Context, yagomodel.Hash)
}

type dhtOutboundRosterCycle struct {
	cycle  dhtOutboundCycle
	roster dhtOutboundPeerRoster
}

func (c dhtOutboundRosterCycle) RunOnce(
	ctx context.Context,
) (dhtexchange.ScheduledDistributionReceipt, error) {
	receipt, err := c.cycle.RunOnce(ctx)
	c.observe(ctx, receipt)
	if err != nil {
		return receipt, fmt.Errorf("run dht outbound cycle: %w", err)
	}

	return receipt, nil
}

func (c dhtOutboundRosterCycle) observe(
	ctx context.Context,
	receipt dhtexchange.ScheduledDistributionReceipt,
) {
	peer := receipt.Distribution.Peer
	if peer == "" {
		return
	}
	if receipt.Distribution.State == dhtexchange.DistributionSent {
		c.roster.ConfirmReachable(ctx, peer)
		slog.DebugContext(
			ctx,
			dhtOutboundPeerConfirmedMessage,
			slog.String("peer", peer.String()),
		)

		return
	}
	if receipt.Distribution.State == dhtexchange.DistributionHandoffRejected {
		logDHTOutboundHandoffRejection(ctx, peer, receipt.Distribution.Handoff)

		return
	}
	if receipt.Retry.Status == dhtexchange.OutboundRetryQuarantined {
		c.roster.ConfirmUnreachable(ctx, peer)
		slog.WarnContext(
			ctx,
			dhtOutboundPeerQuarantinedMessage,
			slog.String("peer", peer.String()),
			slog.Int("failures", receipt.Retry.Failures),
			slog.Time("until", receipt.Retry.QuarantineUntil),
		)
	}
}

func logDHTOutboundHandoffRejection(
	ctx context.Context,
	peer yagomodel.Hash,
	handoff indextransfer.HandoffReceipt,
) {
	stage := "url"
	result := string(handoff.URL.Result)
	pause := 0
	if handoff.State == indextransfer.HandoffRWIRejected {
		stage = "rwi"
		result = string(handoff.RWI.Result)
		pause = handoff.RWI.Pause
	}
	slog.WarnContext(
		ctx,
		dhtOutboundHandoffRejectedMessage,
		slog.String("peer", peer.String()),
		slog.String("stage", stage),
		slog.String("result", result),
		slog.Int("pause", pause),
	)
}
