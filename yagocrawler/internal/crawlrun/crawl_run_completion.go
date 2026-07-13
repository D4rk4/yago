// Package crawlrun tracks outstanding crawl work so that a crawl run's
// completion callback fires exactly when its last page settles, and the crawler
// drains once every intake holder has been released. A crawl order is
// acknowledged through the callback handed to Begin, so the order is acked when
// its run drains.
package crawlrun

import "github.com/google/uuid"

type Completion struct {
	runs   map[uuid.UUID]*outstanding
	intake int
	closed bool
}

type outstanding struct {
	pending int
	failed  bool
	finish  func(succeeded bool)
}

func NewCompletion() *Completion {
	return &Completion{runs: make(map[uuid.UUID]*outstanding)}
}

func (c *Completion) Hold() {
	c.intake++
}

func (c *Completion) Release() (drained bool) {
	c.intake--
	if c.intake == 0 && !c.closed {
		c.closed = true
		return true
	}
	return false
}

func (c *Completion) Begin(runID uuid.UUID, finish func(succeeded bool)) {
	run, ok := c.runs[runID]
	if !ok {
		run = &outstanding{finish: finish}
		c.runs[runID] = run
	}
	run.pending++
}

func (c *Completion) Track(runID uuid.UUID) {
	c.runs[runID].pending++
}

// Fail marks a run as having lost at least one page's references in delivery, so
// its drain reports not-succeeded and the order is naked for redelivery rather
// than acked.
func (c *Completion) Fail(runID uuid.UUID) {
	c.runs[runID].failed = true
}

// Pending reports a run's outstanding page count, or 0 once the run has drained
// and been forgotten, so a progress report can carry a live queue depth.
func (c *Completion) Pending(runID uuid.UUID) int {
	run, ok := c.runs[runID]
	if !ok {
		return 0
	}

	return run.pending
}

func (c *Completion) Settle(
	runID uuid.UUID,
) (finish func(succeeded bool), succeeded, drained bool) {
	return c.SettleMany(runID, 1)
}
