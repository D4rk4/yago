package crawlbroker

import (
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlerActiveFetches struct {
	active     uint32
	observedAt time.Time
}

type CrawlerRuntimeSnapshot struct {
	ConnectedCrawlers              int
	ActiveFetches                  int
	ActiveFetchesKnown             bool
	FetchLimitPerCrawler           int
	AggregateFetchCapacity         int
	StorageStatesKnown             bool
	StorageReportedCrawlers        int
	StorageUnreportedCrawlers      int
	StoragePressured               int
	StorageMeasurementsUnavailable int
	MinimumStorageAvailableBytes   uint64
	StoragePressurePolicy          yagocrawlcontract.StoragePressurePolicy
}

func (r *ControlRegistry) recordActiveFetches(workerID string, activeFetches *uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers[workerID] == 0 {
		return
	}
	if activeFetches == nil || *activeFetches > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		delete(r.activeFetches, workerID)

		return
	}
	r.activeFetches[workerID] = crawlerActiveFetches{
		active:     *activeFetches,
		observedAt: r.now(),
	}
}

func (r *ControlRegistry) RuntimeSnapshot() CrawlerRuntimeSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot := CrawlerRuntimeSnapshot{
		ConnectedCrawlers:     len(r.workers),
		ActiveFetchesKnown:    true,
		FetchLimitPerCrawler:  int(r.fetchWorkers),
		StoragePressurePolicy: r.storagePressurePolicy,
	}
	minimumSet := false
	storageStatesKnown := len(r.workers) > 0
	now := r.now()
	for workerID := range r.workers {
		active, known := r.activeFetches[workerID]
		if !known || now.After(active.observedAt.Add(crawlerHeartbeatReportLifetime)) {
			delete(r.activeFetches, workerID)
			snapshot.ActiveFetchesKnown = false
		} else {
			snapshot.ActiveFetches += int(active.active)
		}
		storage, known := r.storageStates[workerID]
		if !known || now.After(storage.observedAt.Add(crawlerHeartbeatReportLifetime)) {
			delete(r.storageStates, workerID)
			storageStatesKnown = false
			snapshot.StorageUnreportedCrawlers++

			continue
		}
		snapshot.StorageReportedCrawlers++
		if storage.pressured {
			snapshot.StoragePressured++
		}
		if !storage.measurementAvailable {
			snapshot.StorageMeasurementsUnavailable++

			continue
		}
		if !minimumSet || storage.availableBytes < snapshot.MinimumStorageAvailableBytes {
			snapshot.MinimumStorageAvailableBytes = storage.availableBytes
			minimumSet = true
		}
	}
	snapshot.StorageStatesKnown = storageStatesKnown
	snapshot.AggregateFetchCapacity = snapshot.ConnectedCrawlers * snapshot.FetchLimitPerCrawler

	return snapshot
}
