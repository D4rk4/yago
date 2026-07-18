package frontier

import "github.com/google/uuid"

func (f *Frontier) acquireRunDurability(runID uuid.UUID) (*crawlRun, bool) {
	f.mu.Lock()
	run := f.state.runs[runID]
	durable := run != nil && f.persistent(run.provenanceValue) && f.checkpointFailure == nil
	f.mu.Unlock()
	if !durable {
		return run, false
	}
	run.durability.Lock()
	f.mu.Lock()
	current := f.state.runs[runID]
	if current != run || f.checkpointFailure != nil {
		f.mu.Unlock()
		run.durability.Unlock()

		return current, false
	}
	run.awaitingDurability = true
	f.demoteControlBlockedReadyLocked()
	f.refillReadyLocked()
	f.mu.Unlock()
	f.wake()

	return run, true
}

func (f *Frontier) finishRunDurabilityLocked(
	runID uuid.UUID,
	run *crawlRun,
	err error,
) {
	if f.state.runs[runID] == run {
		run.awaitingDurability = false
		if err != nil {
			f.state.completion.Fail(runID)
			f.recordCheckpointFailureLocked(err)
		}
	}
	run.durability.Unlock()
}
