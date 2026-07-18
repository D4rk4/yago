// Package runtally accumulates per-run crawl outcome counts keyed by the run's
// provenance token, so the worker can report a run's fetched/indexed/failed/
// robots-denied/duplicate totals when the run finishes. It is written from the
// pipeline and frontier as pages are processed and read once at run completion.
package runtally

import (
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// Tally holds outcome counts per run. It is safe for concurrent use: pipeline
// workers processing different runs (and the same run across workers) all record
// into it while a run is in flight.
type Tally struct {
	mu    sync.Mutex
	byRun map[string]yagocrawlcontract.CrawlRunTally
}

func New() *Tally {
	return &Tally{byRun: make(map[string]yagocrawlcontract.CrawlRunTally)}
}

func (t *Tally) Commit(provenance []byte, delta yagocrawlcontract.CrawlRunTally) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := string(provenance)
	current := t.byRun[key]
	current.Fetched += delta.Fetched
	current.Indexed += delta.Indexed
	current.Failed += delta.Failed
	current.RobotsDenied += delta.RobotsDenied
	current.Duplicates += delta.Duplicates
	t.byRun[key] = current
}

func (t *Tally) Restore(provenance []byte, tally yagocrawlcontract.CrawlRunTally) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tally.Pending = 0
	t.byRun[string(provenance)] = tally
}

// Snapshot returns the accumulated outcome counts for a run (the Pending field is
// the caller's to set). An unseen run yields a zero tally.
func (t *Tally) Snapshot(provenance []byte) yagocrawlcontract.CrawlRunTally {
	t.mu.Lock()
	defer t.mu.Unlock()
	current, ok := t.byRun[string(provenance)]
	if !ok {
		return yagocrawlcontract.CrawlRunTally{}
	}

	return current
}

// Forget drops a finished run's counts so the map does not grow without bound.
func (t *Tally) Forget(provenance []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byRun, string(provenance))
}
