package crawlrun

import "github.com/google/uuid"

func (c *Completion) SettleMany(
	runID uuid.UUID,
	settled int,
) (finish func(succeeded bool), succeeded, drained bool) {
	run := c.runs[runID]
	run.pending -= settled
	if run.pending == 0 {
		delete(c.runs, runID)

		return run.finish, !run.failed, true
	}

	return nil, false, false
}
