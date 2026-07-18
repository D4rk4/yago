package crawlorder

import (
	"context"
	"log/slog"
)

func (d heartbeatDelivery) confirmRecoveredLeases(
	ctx context.Context,
	leaseIDs []string,
) bool {
	release := d.beginOperation()
	defer release()
	if d.leaseGrants == nil || len(leaseIDs) == 0 {
		return true
	}
	added, err := d.leaseGrants.TrackMany(leaseIDs)
	if err != nil {
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return false
	}
	acknowledged := []uint64(nil)
	if d.acknowledgments != nil {
		acknowledged = d.acknowledgments.snapshot()
	}
	result, err := d.exchange(ctx, acknowledged)
	if err != nil {
		for _, leaseID := range added {
			d.leaseGrants.Revoke(leaseID)
		}
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return false
	}
	if d.acknowledgments != nil {
		d.acknowledgments.confirm(acknowledged)
	}
	applied := d.dispatchDirectives(ctx, result)
	if d.acknowledgments != nil && len(applied) > 0 {
		d.acknowledgments.add(applied)
		d.confirmApplied(ctx)
	}
	for _, leaseID := range leaseIDs {
		if !d.leaseGrants.Confirmed(leaseID) {
			return false
		}
	}

	return true
}
