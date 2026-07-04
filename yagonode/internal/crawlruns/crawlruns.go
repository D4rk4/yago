// Package crawlruns is the node's in-memory registry of crawl runs. A run's live
// state lives in the crawler worker's frontier; this registry mirrors what the
// worker reports so the console can list active and recently finished crawls with
// their outcome tallies, while the durable order backlog stays in the broker.
package crawlruns

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const defaultCapacity = 256

// Run is the node's view of one crawl run, updated from worker progress reports
// and stamped with the node clock so the console can order runs by recency.
type Run struct {
	RunID         string
	WorkerID      string
	ProfileHandle string
	ProfileName   string
	State         yagocrawlcontract.CrawlRunState
	Tally         yagocrawlcontract.CrawlRunTally
	FirstSeen     time.Time
	Updated       time.Time
}

// Registry is a bounded table of crawl runs keyed by run id, safe for concurrent
// use. Records arrive from worker progress reports; the oldest-updated run is
// evicted once the table exceeds its capacity.
type Registry struct {
	mu        sync.Mutex
	runs      map[string]Run
	capacity  int
	now       func() time.Time
	observers []func(run Run, newlyTerminal bool, active int)
}

func isTerminal(state yagocrawlcontract.CrawlRunState) bool {
	return state == yagocrawlcontract.CrawlRunFinished ||
		state == yagocrawlcontract.CrawlRunCancelled
}

// New builds a registry holding at most capacity runs, defaulting a non-positive
// capacity to a sensible bound.
func New(capacity int) *Registry {
	if capacity <= 0 {
		capacity = defaultCapacity
	}

	return &Registry{
		runs:     make(map[string]Run, capacity),
		capacity: capacity,
		now:      time.Now,
	}
}

// Record upserts a run from a worker report, preserving the first-seen time and
// overwriting the mutable fields with the report's absolute snapshot. Reports
// without a run id are ignored.
func (r *Registry) Record(_ context.Context, progress yagocrawlcontract.CrawlRunProgress) {
	if progress.RunID == "" {
		return
	}

	r.mu.Lock()

	now := r.now()
	prev, existed := r.runs[progress.RunID]
	run := prev
	if !existed {
		run.RunID = progress.RunID
		run.FirstSeen = now
	}
	run.WorkerID = progress.WorkerID
	run.ProfileHandle = progress.ProfileHandle
	run.ProfileName = progress.ProfileName
	run.State = progress.State
	run.Tally = progress.Tally
	run.Updated = now
	r.runs[progress.RunID] = run

	r.evictLocked()

	// Detecting the terminal transition under the lock guarantees exactly one
	// newly-terminal notification per run, even under concurrent reports.
	newlyTerminal := isTerminal(run.State) && (!existed || !isTerminal(prev.State))
	active := r.activeCountLocked()
	observers := r.observers
	r.mu.Unlock()

	for _, observe := range observers {
		observe(run, newlyTerminal, active)
	}
}

// AddObserver registers a callback invoked after each recorded report with the
// run's current snapshot, whether this report was its first terminal transition,
// and the number of runs still active. Observers are registered at assembly time,
// before the broker serves, so registration never races a report.
func (r *Registry) AddObserver(observe func(run Run, newlyTerminal bool, active int)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.observers = append(r.observers, observe)
}

func (r *Registry) activeCountLocked() int {
	active := 0
	for _, run := range r.runs {
		if !isTerminal(run.State) {
			active++
		}
	}

	return active
}

// Recent returns the runs newest-updated first, breaking ties by run id.
func (r *Registry) Recent() []Run {
	r.mu.Lock()
	defer r.mu.Unlock()

	runs := make([]Run, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Updated.Equal(runs[j].Updated) {
			return runs[i].RunID < runs[j].RunID
		}

		return runs[i].Updated.After(runs[j].Updated)
	})

	return runs
}

// Len reports how many runs the registry currently holds.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.runs)
}

func (r *Registry) evictLocked() {
	for len(r.runs) > r.capacity {
		var oldestID string
		var oldest time.Time
		first := true
		for id, run := range r.runs {
			if first || run.Updated.Before(oldest) {
				oldestID = id
				oldest = run.Updated
				first = false
			}
		}
		delete(r.runs, oldestID)
	}
}
