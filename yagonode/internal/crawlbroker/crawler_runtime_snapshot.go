package crawlbroker

import (
	"sort"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlerActiveFetches struct {
	processIdentifier string
	activeKnown       bool
	active            uint32
	waiting           uint32
	waitingKnown      bool
	activePages       []CrawlerActivePage
	waitingPages      []CrawlerWaitingPage
	observedAt        time.Time
}

type CrawlerActivePage struct {
	RunID               string
	URL                 string
	Stage               string
	ElapsedMilliseconds uint64
}

type CrawlerWaitingPage struct {
	RunID       string
	URL         string
	State       string
	Reason      string
	AvailableAt time.Time
}

type CrawlerWorkerSnapshot struct {
	WorkerID                   string
	ProcessIdentifier          string
	ActiveFetches              uint32
	ActiveFetchesKnown         bool
	FetchAdmissionWaiting      uint32
	FetchAdmissionWaitingKnown bool
	ActivePages                []CrawlerActivePage
	WaitingPages               []CrawlerWaitingPage
	ObservedAt                 time.Time
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
	r.recordHeartbeatFetchActivity(workerID, "", activeFetches, nil)
}

func (r *ControlRegistry) recordHeartbeatFetchActivity(
	workerID string,
	processIdentifier string,
	activeFetches *uint32,
	fetchAdmissionWaiting *uint32,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers[workerID] == 0 {
		return
	}
	if activeFetches == nil {
		r.activeFetches[workerID] = crawlerActiveFetches{
			processIdentifier: processIdentifier,
			observedAt:        r.now(),
		}

		return
	}
	if *activeFetches > yagocrawlcontract.MaximumFetchWorkerConcurrency ||
		(fetchAdmissionWaiting != nil && *fetchAdmissionWaiting > *activeFetches) {
		delete(r.activeFetches, workerID)

		return
	}
	waiting := uint32(0)
	if fetchAdmissionWaiting != nil {
		waiting = *fetchAdmissionWaiting
	}
	r.activeFetches[workerID] = crawlerActiveFetches{
		processIdentifier: processIdentifier,
		activeKnown:       true,
		active:            *activeFetches,
		waiting:           waiting,
		waitingKnown:      fetchAdmissionWaiting != nil,
		observedAt:        r.now(),
	}
}

func (r *ControlRegistry) recordHeartbeatActivePages(
	workerID string,
	processIdentifier string,
	activePages []CrawlerActivePage,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers[workerID] == 0 {
		return
	}
	activity := r.activeFetches[workerID]
	if processIdentifier != "" {
		activity.processIdentifier = processIdentifier
	}
	activity.activePages = append([]CrawlerActivePage(nil), activePages...)
	activity.observedAt = r.now()
	r.activeFetches[workerID] = activity
}

func (r *ControlRegistry) recordHeartbeatWaitingPages(
	workerID string,
	processIdentifier string,
	waitingPages []CrawlerWaitingPage,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers[workerID] == 0 {
		return
	}
	activity := r.activeFetches[workerID]
	if processIdentifier != "" {
		activity.processIdentifier = processIdentifier
	}
	activity.waitingPages = append([]CrawlerWaitingPage(nil), waitingPages...)
	activity.observedAt = r.now()
	r.activeFetches[workerID] = activity
}

func (r *ControlRegistry) WorkerSnapshots() []CrawlerWorkerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	snapshots := make([]CrawlerWorkerSnapshot, 0, len(r.workers))
	for workerID := range r.workers {
		snapshot := CrawlerWorkerSnapshot{WorkerID: workerID}
		activity, found := r.activeFetches[workerID]
		if found {
			snapshot.ProcessIdentifier = activity.processIdentifier
			snapshot.ObservedAt = activity.observedAt
		}
		if found && !now.After(activity.observedAt.Add(crawlerHeartbeatReportLifetime)) {
			snapshot.ActiveFetches = activity.active
			snapshot.ActiveFetchesKnown = activity.activeKnown
			snapshot.FetchAdmissionWaiting = activity.waiting
			snapshot.FetchAdmissionWaitingKnown = activity.waitingKnown
			snapshot.ActivePages = append([]CrawlerActivePage(nil), activity.activePages...)
			snapshot.WaitingPages = append([]CrawlerWaitingPage(nil), activity.waitingPages...)
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].WorkerID < snapshots[j].WorkerID
	})

	return snapshots
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
		if !known || !active.activeKnown ||
			now.After(active.observedAt.Add(crawlerHeartbeatReportLifetime)) {
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
