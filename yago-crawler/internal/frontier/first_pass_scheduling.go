package frontier

import "github.com/google/uuid"

func (f *Frontier) hasFirstPassWorkLocked() bool {
	for runID, run := range f.state.runs {
		if !firstPassRunSelectable(f, runID, run) {
			continue
		}
		if run.seeding || run.firstPassPending > 0 ||
			(run.boundedRecovery && !run.recoveryComplete) {
			return true
		}
	}
	return false
}

func firstPassRunSelectable(f *Frontier, runID uuid.UUID, run *crawlRun) bool {
	if run == nil {
		return false
	}
	_, suspended := f.suspended[run.provenance]
	return !run.cancelled &&
		!f.isPausedLocked(run.provenanceValue) &&
		!suspended &&
		f.state.runs[runID] == run
}
