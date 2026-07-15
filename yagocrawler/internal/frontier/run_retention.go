package frontier

import "github.com/google/uuid"

func (f *Frontier) cleanupRunLocked(runID uuid.UUID) {
	run := f.state.runs[runID]
	provenance := run.provenance
	delete(f.state.runs, runID)
	delete(f.readyPerRun, runID)
	delete(f.dispatchOrder, runID)
	delete(f.readyOrder, runID)
	for _, active := range f.state.runs {
		if active.provenance == provenance {
			return
		}
	}
	delete(f.paused, provenance)
	delete(f.controlSeen, provenance)
	delete(f.rateInterval, provenance)
	delete(f.pagesPerMinute, provenance)
	delete(f.rateNextDue, provenance)
}
