package crawlorder

import (
	"context"
	"log/slog"
	"sync"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type heartbeatDelivery struct {
	client          OrderStreamer
	workerID        string
	workerSessionID string
	control         ControlHandler
	activeFetches   func() uint32
	acknowledgments *controlAcknowledgments
	leaseGrants     *crawllease.GrantRegistry
	operation       sync.Locker
	storageSnapshot func() yagocrawlcontract.StoragePressureSnapshot
	storagePolicy   func(yagocrawlcontract.StoragePressurePolicy)
}

func (d heartbeatDelivery) deliver(ctx context.Context) {
	release := d.beginOperation()
	defer release()
	acknowledged := []uint64(nil)
	if d.acknowledgments != nil {
		acknowledged = d.acknowledgments.snapshot()
	}
	result, err := d.exchange(ctx, acknowledged)
	if err != nil {
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return
	}
	if d.acknowledgments != nil {
		d.acknowledgments.confirm(acknowledged)
	}
	applied := d.dispatchDirectives(ctx, result)
	if d.acknowledgments == nil || len(applied) == 0 {
		return
	}
	d.acknowledgments.add(applied)
	d.confirmApplied(ctx)
}

func (d heartbeatDelivery) confirmLease(ctx context.Context, leaseID string) bool {
	release := d.beginOperation()
	defer release()
	if d.leaseGrants == nil {
		return true
	}
	if err := d.leaseGrants.Track(leaseID); err != nil {
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return false
	}
	acknowledged := []uint64(nil)
	if d.acknowledgments != nil {
		acknowledged = d.acknowledgments.snapshot()
	}
	confirmedLeaseIDs := []string{leaseID}
	result, err := d.exchangeForLeases(ctx, acknowledged, confirmedLeaseIDs)
	if err != nil {
		d.leaseGrants.Revoke(leaseID)
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return false
	}
	if d.acknowledgments != nil {
		d.acknowledgments.confirm(acknowledged)
	}
	applied := d.dispatchDirectives(ctx, result)
	if d.acknowledgments != nil && len(applied) > 0 {
		d.acknowledgments.add(applied)
		d.confirmAppliedForLeases(ctx, confirmedLeaseIDs)
	}
	if !d.leaseGrants.Confirmed(leaseID) {
		d.leaseGrants.Revoke(leaseID)

		return false
	}

	return true
}

func (d heartbeatDelivery) exchange(
	ctx context.Context,
	acknowledged []uint64,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	activeLeases := []string(nil)
	if d.leaseGrants != nil {
		activeLeases = d.leaseGrants.ActiveLeaseIDs()
	}

	return d.exchangeForLeases(ctx, acknowledged, activeLeases)
}

func (d heartbeatDelivery) confirmApplied(ctx context.Context) {
	d.confirmAppliedForLeases(ctx, nil)
}

func (d heartbeatDelivery) confirmAppliedForLeases(
	ctx context.Context,
	leaseIDs []string,
) {
	acknowledgmentContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		orderAckTimeout,
	)
	defer cancel()
	acknowledged := d.acknowledgments.snapshot()
	var err error
	if leaseIDs == nil {
		_, err = d.exchange(acknowledgmentContext, acknowledged)
	} else {
		_, err = d.exchangeForLeases(acknowledgmentContext, acknowledged, leaseIDs)
	}
	if err != nil {
		slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

		return
	}
	d.acknowledgments.confirm(acknowledged)
}

func (d heartbeatDelivery) beginOperation() func() {
	if d.operation == nil {
		return func() {}
	}
	d.operation.Lock()

	return d.operation.Unlock
}

func (d heartbeatDelivery) dispatchDirectives(
	ctx context.Context,
	result *crawlrpc.WorkerHeartbeatResult,
) []uint64 {
	directives := result.GetDirectives()
	if d.acknowledgments != nil {
		directives = directives[:min(len(directives), d.acknowledgments.available())]
	}

	return dispatchDirectives(ctx, d.control, directives)
}
