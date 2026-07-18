package crawlbroker

import (
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const crawlerHeartbeatReportLifetime = 3 * yagocrawlcontract.DefaultWorkerHeartbeatInterval

type GrowthAdmission interface {
	CheckGrowth() error
}

type crawlerStorageState struct {
	availableBytes       uint64
	measurementAvailable bool
	pressured            bool
	observedAt           time.Time
}

func (r *ControlRegistry) SetStoragePressurePolicy(
	policy yagocrawlcontract.StoragePressurePolicy,
) {
	r.mu.Lock()
	r.storagePressurePolicy = policy
	r.mu.Unlock()
}

func (r *ControlRegistry) StoragePressurePolicy() yagocrawlcontract.StoragePressurePolicy {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.storagePressurePolicy
}

func (r *ControlRegistry) recordStoragePressure(
	workerID string,
	heartbeat *crawlrpc.WorkerHeartbeat,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workers[workerID] == 0 {
		return
	}
	if heartbeat.StorageAvailableBytes == nil ||
		heartbeat.StoragePressure == nil ||
		heartbeat.StorageMeasurementAvailable == nil {
		delete(r.storageStates, workerID)

		return
	}
	r.storageStates[workerID] = crawlerStorageState{
		availableBytes:       heartbeat.GetStorageAvailableBytes(),
		measurementAvailable: heartbeat.GetStorageMeasurementAvailable(),
		pressured:            heartbeat.GetStoragePressure(),
		observedAt:           r.now(),
	}
}
