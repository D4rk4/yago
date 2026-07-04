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

func (c *Completion) Fail(runID uuid.UUID) {
	c.runs[runID].failed = true
}

func (c *Completion) Settle(
	runID uuid.UUID,
) (finish func(succeeded bool), succeeded, drained bool) {
	run := c.runs[runID]
	run.pending--
	if run.pending == 0 {
		delete(c.runs, runID)
		return run.finish, !run.failed, true
	}
	return nil, false, false
}
