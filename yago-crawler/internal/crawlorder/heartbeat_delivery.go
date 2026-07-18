package crawlorder

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	operation       *sync.Mutex
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
	result, err := d.exchange(ctx, acknowledged)
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
		d.confirmApplied(ctx)
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
	requestStarted := time.Now()
	heartbeatCtx, cancelHeartbeat := boundedHeartbeatContext(ctx)
	defer cancelHeartbeat()
	heartbeat := workerSessionHeartbeat(
		d.workerID,
		d.workerSessionID,
		d.activeFetches,
		activeLeases,
		acknowledged...,
	)
	if d.storageSnapshot != nil {
		snapshot := d.storageSnapshot()
		available := snapshot.AvailableBytes
		pressured := snapshot.Pressured
		measurementAvailable := snapshot.MeasurementAvailable
		heartbeat.StorageAvailableBytes = &available
		heartbeat.StoragePressure = &pressured
		heartbeat.StorageMeasurementAvailable = &measurementAvailable
	}
	result, err := d.client.Heartbeat(
		heartbeatCtx,
		heartbeat,
	)
	if err != nil {
		if d.leaseGrants != nil && status.Code(err) == codes.FailedPrecondition {
			for _, leaseID := range activeLeases {
				d.leaseGrants.Reject(leaseID)
			}
		}

		return nil, fmt.Errorf("deliver crawler heartbeat: %w", err)
	}
	if d.storagePolicy != nil && result.StorageReservedFreeBytes != nil &&
		result.StoragePressureHysteresisBytes != nil {
		d.storagePolicy(yagocrawlcontract.StoragePressurePolicy{
			ReservedFreeBytes:       result.GetStorageReservedFreeBytes(),
			RecoveryHysteresisBytes: result.GetStoragePressureHysteresisBytes(),
		})
	}
	if d.leaseGrants != nil {
		leaseTTL, err := heartbeatLeaseTTL(result.GetLeaseTtlMilliseconds())
		if err != nil {
			return nil, err
		}
		d.leaseGrants.Renew(
			requestStarted,
			leaseTTL,
			activeLeases,
			result.GetRenewedLeaseIds(),
		)
	}

	return result, nil
}

func heartbeatLeaseTTL(milliseconds uint64) (time.Duration, error) {
	maximumMilliseconds := uint64(math.MaxInt64 / int64(time.Millisecond))
	if milliseconds > maximumMilliseconds {
		return 0, fmt.Errorf("deliver crawler heartbeat: lease duration is out of range")
	}

	return time.Duration(milliseconds) * time.Millisecond, nil
}

func (d heartbeatDelivery) confirmApplied(ctx context.Context) {
	acknowledgmentContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		orderAckTimeout,
	)
	defer cancel()
	acknowledged := d.acknowledgments.snapshot()
	_, err := d.exchange(acknowledgmentContext, acknowledged)
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
