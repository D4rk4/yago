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
	RunID          string
	WorkerID       string
	ProfileHandle  string
	ProfileName    string
	State          yagocrawlcontract.CrawlRunState
	Tally          yagocrawlcontract.CrawlRunTally
	FirstSeen      time.Time
	Updated        time.Time
	PagesPerMinute uint32
	RateKnown      bool
}

// Registry is a concurrent crawl-run table that retains every active run and
// bounds only recent terminal history by capacity.
type Registry struct {
	mu         sync.Mutex
	runs       map[string]Run
	capacity   int
	activeRuns int
	now        func() time.Time
	observers  []func(run Run, newlyTerminal bool, active int)
	terminal   *terminalDeliveryLedger
}

func isTerminal(state yagocrawlcontract.CrawlRunState) bool {
	return state == yagocrawlcontract.CrawlRunFinished ||
		state == yagocrawlcontract.CrawlRunCancelled
}

// New builds a registry whose capacity bounds recent terminal history and
// defaults a non-positive capacity to a sensible bound.
func New(capacity int) *Registry {
	if capacity <= 0 {
		capacity = defaultCapacity
	}

	return &Registry{
		runs:     make(map[string]Run, capacity),
		capacity: capacity,
		now:      time.Now,
		terminal: newTerminalDeliveryLedger(),
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
	if progress.RateKnown {
		run.PagesPerMinute = progress.PagesPerMinute
		run.RateKnown = true
	}
	run.Updated = now
	r.reconcileRunActivityLocked(existed, prev, run)
	r.runs[progress.RunID] = run

	r.evictLocked()

	newlyTerminal := isTerminal(run.State) && (!existed || !isTerminal(prev.State))
	active := r.activeCountLocked()
	observers := r.observers
	r.mu.Unlock()

	for _, observe := range observers {
		observe(run, newlyTerminal, active)
	}
}

func (r *Registry) SetRate(runID string, pagesPerMinute uint32) {
	if runID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	run, ok := r.runs[runID]
	if !ok {
		return
	}
	run.PagesPerMinute = pagesPerMinute
	run.RateKnown = true
	r.runs[runID] = run
}

func (r *Registry) AddObserver(observe func(run Run, newlyTerminal bool, active int)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.observers = append(r.observers, observe)
}

func (r *Registry) activeCountLocked() int {
	return r.activeRuns
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
