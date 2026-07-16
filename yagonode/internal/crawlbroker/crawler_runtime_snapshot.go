package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

type CrawlerRuntimeSnapshot struct {
	ConnectedCrawlers      int
	ActiveFetches          int
	ActiveFetchesKnown     bool
	FetchLimitPerCrawler   int
	AggregateFetchCapacity int
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
	r.activeFetches[workerID] = *activeFetches
}

func (r *ControlRegistry) RuntimeSnapshot() CrawlerRuntimeSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot := CrawlerRuntimeSnapshot{
		ConnectedCrawlers:    len(r.workers),
		ActiveFetchesKnown:   true,
		FetchLimitPerCrawler: int(r.fetchWorkers),
	}
	for workerID := range r.workers {
		active, known := r.activeFetches[workerID]
		if !known {
			snapshot.ActiveFetchesKnown = false

			continue
		}
		snapshot.ActiveFetches += int(active)
	}
	snapshot.AggregateFetchCapacity = snapshot.ConnectedCrawlers * snapshot.FetchLimitPerCrawler

	return snapshot
}
