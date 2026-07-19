package crawlorder

import (
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (d heartbeatDelivery) leaseHeartbeat(
	activeLeaseIDs []string,
	acknowledged []uint64,
	confirmDeliveries bool,
) *crawlrpc.WorkerHeartbeat {
	heartbeat := workerSessionHeartbeat(
		d.workerID,
		d.workerSessionID,
		d.activeFetches,
		activeLeaseIDs,
		acknowledged...,
	)
	heartbeat.ConfirmActiveLeaseDeliveries = &confirmDeliveries
	if d.urlDenylist != nil {
		heartbeat.UrlDenylistRevision = d.urlDenylist.Revision()
	}
	if d.storageSnapshot != nil {
		snapshot := d.storageSnapshot()
		available := snapshot.AvailableBytes
		pressured := snapshot.Pressured
		measurementAvailable := snapshot.MeasurementAvailable
		heartbeat.StorageAvailableBytes = &available
		heartbeat.StoragePressure = &pressured
		heartbeat.StorageMeasurementAvailable = &measurementAvailable
	}

	return heartbeat
}

func (d heartbeatDelivery) rejectLeasesAfterHeartbeatError(err error) {
	if d.leaseGrants == nil || status.Code(err) != codes.FailedPrecondition {
		return
	}
	for _, leaseID := range d.leaseGrants.ActiveLeaseIDs() {
		d.leaseGrants.Reject(leaseID)
	}
}

func (d heartbeatDelivery) applyHeartbeatRuntimePolicy(
	result *crawlrpc.WorkerHeartbeatResult,
) error {
	if d.runtimePolicy == nil || result.GetRuntimePolicy() == nil {
		return nil
	}
	fallback := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	if d.runtimePolicySource != nil {
		fallback = d.runtimePolicySource()
	}
	policy, err := yagocrawlcontract.CrawlerRuntimePolicyFromProtoWithFallback(
		result.GetRuntimePolicy(),
		fallback,
	)
	if err != nil {
		return fmt.Errorf("deliver crawler heartbeat: %w", err)
	}
	d.runtimePolicy(policy)

	return nil
}

func (d heartbeatDelivery) applyHeartbeatStoragePolicy(
	result *crawlrpc.WorkerHeartbeatResult,
) {
	if d.storagePolicy == nil || result.StorageReservedFreeBytes == nil ||
		result.StoragePressureHysteresisBytes == nil {
		return
	}
	d.storagePolicy(yagocrawlcontract.StoragePressurePolicy{
		ReservedFreeBytes:       result.GetStorageReservedFreeBytes(),
		RecoveryHysteresisBytes: result.GetStoragePressureHysteresisBytes(),
	})
}

func (d heartbeatDelivery) renewHeartbeatLeases(
	requestStarted time.Time,
	activeLeaseIDs []string,
	result *crawlrpc.WorkerHeartbeatResult,
) error {
	if d.leaseGrants == nil {
		return nil
	}
	leaseTTL, err := heartbeatLeaseTTL(result.GetLeaseTtlMilliseconds())
	if err != nil {
		return err
	}
	d.leaseGrants.Renew(
		requestStarted,
		leaseTTL,
		activeLeaseIDs,
		result.GetRenewedLeaseIds(),
	)

	return nil
}
