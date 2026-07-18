package frontier

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type pendingControlUpdate struct {
	pauseSet       bool
	paused         bool
	cancelled      bool
	rateSet        bool
	pagesPerMinute uint32
}

func pauseControlUpdate(paused bool) pendingControlUpdate {
	return pendingControlUpdate{pauseSet: true, paused: paused}
}

func cancelControlUpdate() pendingControlUpdate {
	return pendingControlUpdate{pauseSet: true, paused: true, cancelled: true}
}

func rateControlUpdate(pagesPerMinute uint32) pendingControlUpdate {
	return pendingControlUpdate{rateSet: true, pagesPerMinute: pagesPerMinute}
}

func (f *Frontier) applyRunControlLocked(key string, update pendingControlUpdate) {
	if !f.hasProvenanceLocked(key) {
		f.retainPendingControlLocked(key)
		f.pendingControl[key] = mergeControlUpdate(f.pendingControl[key], update)
		f.applyControlMemoryLocked(key, update)

		return
	}
	delete(f.controlSeen, key)
	delete(f.pendingControl, key)
	f.applyControlMemoryLocked(key, update)
}

func (f *Frontier) applyRunControl(key string, update pendingControlUpdate) (bool, bool) {
	f.controlOrder.Lock()
	defer f.controlOrder.Unlock()
	f.mu.Lock()
	var runID uuid.UUID
	for candidateID, run := range f.state.runs {
		if run.provenance == key {
			runID = candidateID

			break
		}
	}
	if runID == uuid.Nil {
		f.applyRunControlLocked(key, update)
		f.mu.Unlock()

		return true, false
	}
	f.mu.Unlock()
	run, durable := f.acquireRunDurability(runID)
	if run == nil {
		return true, false
	}
	var checkpointErr error
	if durable {
		checkpointErr = f.checkpoint.UpdateControl(
			context.Background(),
			[]byte(key),
			checkpointControlUpdate(update),
		)
	}
	f.mu.Lock()
	if durable && checkpointErr != nil {
		f.failProvenanceLocked(key)
		f.finishRunDurabilityLocked(runID, run, checkpointErr)
		f.mu.Unlock()
		f.wake()

		return false, false
	}
	f.applyRunControlLocked(key, update)
	if durable {
		f.finishRunDurabilityLocked(runID, run, nil)
	}
	f.mu.Unlock()
	f.wake()

	return true, durable
}

func mergeControlUpdate(current, update pendingControlUpdate) pendingControlUpdate {
	if update.pauseSet {
		current.pauseSet = true
		current.paused = update.paused
	}
	current.cancelled = current.cancelled || update.cancelled
	if update.rateSet {
		current.rateSet = true
		current.pagesPerMinute = update.pagesPerMinute
	}

	return current
}

func checkpointControlUpdate(update pendingControlUpdate) frontiercheckpoint.ControlUpdate {
	converted := frontiercheckpoint.ControlUpdate{Cancelled: update.cancelled}
	if update.pauseSet {
		paused := update.paused
		converted.Paused = &paused
	}
	if update.rateSet {
		pagesPerMinute := update.pagesPerMinute
		converted.PagesPerMinute = &pagesPerMinute
	}

	return converted
}

func (f *Frontier) applyControlMemoryLocked(key string, update pendingControlUpdate) {
	if update.pauseSet {
		if update.paused {
			f.paused[key] = struct{}{}
		} else {
			delete(f.paused, key)
		}
	}
	if update.cancelled {
		f.state.cancelled[key] = struct{}{}
		for _, run := range f.state.runs {
			if run.provenance == key && !run.cancelled {
				run.cancelled = true
				f.cancelRuns[key]++
			}
		}
	}
	if update.rateSet {
		if update.pagesPerMinute == 0 {
			f.rateInterval[key] = 0
			delete(f.rateNextDue, key)
		} else {
			f.rateInterval[key] = time.Minute / time.Duration(update.pagesPerMinute)
		}
		f.pagesPerMinute[key] = update.pagesPerMinute
	}
}

func (f *Frontier) restoreControlStateLocked(
	runID uuid.UUID,
	control frontiercheckpoint.RunControl,
) {
	run := f.state.runs[runID]
	key := run.provenance
	if control.Paused {
		f.paused[key] = struct{}{}
	} else {
		delete(f.paused, key)
	}
	if control.Cancelled {
		f.applyControlMemoryLocked(key, cancelControlUpdate())
	}
	if control.PagesPerMinute != nil {
		f.applyControlMemoryLocked(key, rateControlUpdate(*control.PagesPerMinute))
	}
	if interval := f.rateIntervalLocked(key); interval > 0 {
		f.rateNextDue[key] = time.Now().Add(interval)
	} else {
		delete(f.rateNextDue, key)
	}
}

func (f *Frontier) persistPendingControl(runID uuid.UUID) {
	f.mu.Lock()
	run := f.state.runs[runID]
	if run == nil {
		f.mu.Unlock()

		return
	}
	key := run.provenance
	update, found := f.pendingControl[key]
	if !found {
		delete(f.controlSeen, key)
		f.mu.Unlock()

		return
	}
	f.mu.Unlock()
	run, durable := f.acquireRunDurability(runID)
	var checkpointErr error
	if durable {
		checkpointErr = f.checkpoint.UpdateControl(
			context.Background(),
			run.provenanceValue,
			checkpointControlUpdate(update),
		)
	}
	f.mu.Lock()
	if durable && checkpointErr != nil {
		f.finishRunDurabilityLocked(runID, run, checkpointErr)
		f.mu.Unlock()
		f.wake()

		return
	}
	pageURLs := make([]string, 0)
	if f.state.runs[runID] == run {
		f.applyControlMemoryLocked(key, update)
		if update.cancelled {
			pageURLs = f.queuedProvenanceURLsLocked(key)
		}
	}
	f.mu.Unlock()
	if durable && update.cancelled {
		checkpoint, supported := f.checkpoint.(queuedPageCancellationCheckpoint)
		if !supported {
			checkpointErr = frontiercheckpoint.ErrCorruptCheckpoint
		} else {
			checkpointErr = checkpoint.CancelQueuedPages(
				context.Background(),
				run.provenanceValue,
				pageURLs,
			)
		}
	}
	f.mu.Lock()
	if durable && checkpointErr != nil {
		f.finishRunDurabilityLocked(runID, run, checkpointErr)
		f.mu.Unlock()
		f.wake()

		return
	}
	if f.state.runs[runID] == run {
		delete(f.pendingControl, key)
		delete(f.controlSeen, key)
	}
	if durable {
		f.finishRunDurabilityLocked(runID, run, nil)
	}
	f.mu.Unlock()
	f.wake()
}

func (f *Frontier) failProvenanceLocked(provenance string) {
	for runID, run := range f.state.runs {
		if run.provenance == provenance {
			f.state.completion.Fail(runID)
		}
	}
}
