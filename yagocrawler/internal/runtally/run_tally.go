// Package runtally accumulates per-run crawl outcome counts keyed by the run's
// provenance token, so the worker can report a run's fetched/indexed/failed/
// robots-denied/duplicate totals when the run finishes. It is written from the
// pipeline and frontier as pages are processed and read once at run completion.
package runtally

import (
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type counts struct {
	fetched      uint64
	indexed      uint64
	failed       uint64
	robotsDenied uint64
	duplicates   uint64
}

// Tally holds outcome counts per run. It is safe for concurrent use: pipeline
// workers processing different runs (and the same run across workers) all record
// into it while a run is in flight.
type Tally struct {
	mu    sync.Mutex
	byRun map[string]*counts
}

func New() *Tally {
	return &Tally{byRun: make(map[string]*counts)}
}

func (t *Tally) Fetched(provenance []byte) {
	t.bump(provenance, func(c *counts) { c.fetched++ })
}

func (t *Tally) Indexed(provenance []byte) {
	t.bump(provenance, func(c *counts) { c.indexed++ })
}

func (t *Tally) Failed(provenance []byte) {
	t.bump(provenance, func(c *counts) { c.failed++ })
}

func (t *Tally) RobotsDenied(provenance []byte) {
	t.bump(provenance, func(c *counts) { c.robotsDenied++ })
}

func (t *Tally) Duplicate(provenance []byte) {
	t.bump(provenance, func(c *counts) { c.duplicates++ })
}

func (t *Tally) bump(provenance []byte, apply func(*counts)) {
	key := string(provenance)
	t.mu.Lock()
	defer t.mu.Unlock()
	current, ok := t.byRun[key]
	if !ok {
		current = &counts{}
		t.byRun[key] = current
	}
	apply(current)
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

	return yagocrawlcontract.CrawlRunTally{
		Fetched:      current.fetched,
		Indexed:      current.indexed,
		Failed:       current.failed,
		RobotsDenied: current.robotsDenied,
		Duplicates:   current.duplicates,
	}
}

// Forget drops a finished run's counts so the map does not grow without bound.
func (t *Tally) Forget(provenance []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byRun, string(provenance))
}
