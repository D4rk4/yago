package frontier

import (
	"bytes"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

type RunLeaseRebindResult uint8

const (
	RunLeaseBindingConflict RunLeaseRebindResult = iota
	RunLeaseRebound
	RunLeaseAlreadyComplete
)

func (f *Frontier) RebindRunLease(
	provenance []byte,
	expectedLeaseID string,
	replacementLeaseID string,
) RunLeaseRebindResult {
	if len(provenance) == 0 || expectedLeaseID == "" || replacementLeaseID == "" {
		return RunLeaseBindingConflict
	}
	f.mu.Lock()
	runID, run, found, unique := f.runByProvenanceLocked(provenance)
	f.mu.Unlock()
	if !found {
		return RunLeaseAlreadyComplete
	}
	if !unique {
		return RunLeaseBindingConflict
	}
	run.durability.Lock()
	defer run.durability.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.rebindSelectedRunLeaseLocked(
		provenance,
		runID,
		run,
		expectedLeaseID,
		replacementLeaseID,
	)
}

func (f *Frontier) rebindSelectedRunLeaseLocked(
	provenance []byte,
	selectedRunID uuid.UUID,
	selectedRun *crawlRun,
	expectedLeaseID string,
	replacementLeaseID string,
) RunLeaseRebindResult {
	currentRunID, current, currentFound, currentUnique := f.runByProvenanceLocked(provenance)
	if !currentFound {
		return RunLeaseAlreadyComplete
	}
	if !currentUnique || currentRunID != selectedRunID || current != selectedRun {
		return RunLeaseBindingConflict
	}
	if selectedRun.leaseID == replacementLeaseID {
		return RunLeaseRebound
	}
	if selectedRun.leaseID != expectedLeaseID || !f.readyLeaseBindingValidLocked(
		selectedRunID,
		expectedLeaseID,
		replacementLeaseID,
	) {
		return RunLeaseBindingConflict
	}
	selectedRun.leaseID = replacementLeaseID
	for index := range f.state.ready {
		if f.state.ready[index].RunID == selectedRunID {
			f.state.ready[index].LeaseID = replacementLeaseID
		}
	}
	f.signalLeaseBindingChangeLocked()
	f.wake()

	return RunLeaseRebound
}

func (f *Frontier) runByProvenanceLocked(
	provenance []byte,
) (uuid.UUID, *crawlRun, bool, bool) {
	var selectedID uuid.UUID
	var selected *crawlRun
	for runID, run := range f.state.runs {
		if !bytes.Equal(run.provenanceValue, provenance) {
			continue
		}
		if selected != nil {
			return uuid.Nil, nil, true, false
		}
		selectedID = runID
		selected = run
	}

	return selectedID, selected, selected != nil, true
}

func (f *Frontier) readyLeaseBindingValidLocked(
	runID uuid.UUID,
	expectedLeaseID string,
	replacementLeaseID string,
) bool {
	for _, job := range f.state.ready {
		if job.RunID == runID && job.LeaseID != expectedLeaseID &&
			job.LeaseID != replacementLeaseID {
			return false
		}
	}

	return true
}

func runLeaseMatchesJob(run *crawlRun, work crawljob.CrawlJob) bool {
	return run != nil && run.leaseID == work.LeaseID
}

func (f *Frontier) abandonJobLocked(work crawljob.CrawlJob, run *crawlRun) {
	f.releaseHost(work.URL)
	if run == nil {
		return
	}
	run.clearPageHostProgress(work)
	candidate := candidateFromJob(work)
	run.prependReturned(candidate.host, []pendingPage{pendingPageFromCandidate(candidate)})
	f.refillReadyLocked()
}

func (f *Frontier) abandonStaleLeaseJob(
	work crawljob.CrawlJob,
	run *crawlRun,
	durable bool,
) bool {
	f.mu.Lock()
	if f.state.runs[work.RunID] != run || runLeaseMatchesJob(run, work) {
		f.mu.Unlock()

		return false
	}
	f.abandonJobLocked(work, run)
	if durable {
		f.finishRunDurabilityLocked(work.RunID, run, nil)
	}
	f.mu.Unlock()
	f.wake()

	return true
}
