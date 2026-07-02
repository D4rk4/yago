package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
)

const (
	dhtOutboundPeerConfirmedMessage           = "dht outbound peer confirmed"
	dhtOutboundPeerRemoteIndexRejectedMessage = "dht outbound peer remote index rejected"
	dhtOutboundPeerQuarantinedMessage         = "dht outbound peer quarantined"
)

type dhtOutboundPeerRoster interface {
	ConfirmReachable(context.Context, yacymodel.Hash)
	ConfirmUnreachable(context.Context, yacymodel.Hash)
	RejectRemoteIndex(context.Context, yacymodel.Seed)
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
	if receipt.Distribution.State == dhtexchange.DistributionHandoffRejected &&
		receipt.Distribution.Target.Hash != "" {
		c.roster.RejectRemoteIndex(ctx, receipt.Distribution.Target)
		slog.WarnContext(
			ctx,
			dhtOutboundPeerRemoteIndexRejectedMessage,
			slog.String("peer", peer.String()),
		)
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
