package crawlorder

import (
	"context"
	"fmt"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (d heartbeatDelivery) exchangeForLeases(
	ctx context.Context,
	acknowledged []uint64,
	activeLeaseIDs []string,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	requestStarted := time.Now()
	heartbeatCtx, cancelHeartbeat := boundedHeartbeatContext(ctx)
	defer cancelHeartbeat()
	heartbeat := workerSessionHeartbeat(
		d.workerID,
		d.workerSessionID,
		d.activeFetches,
		activeLeaseIDs,
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
	result, err := d.client.Heartbeat(heartbeatCtx, heartbeat)
	if err != nil {
		if d.leaseGrants != nil && status.Code(err) == codes.FailedPrecondition {
			for _, leaseID := range d.leaseGrants.ActiveLeaseIDs() {
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
			activeLeaseIDs,
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
