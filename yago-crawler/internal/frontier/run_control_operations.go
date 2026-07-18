package frontier

import "time"

func (f *Frontier) Pause(provenance []byte) {
	f.pauseControl(provenance)
}

func (f *Frontier) PauseControl(provenance []byte) bool {
	return f.pauseControl(provenance)
}

func (f *Frontier) pauseControl(provenance []byte) bool {
	applied, durable := f.applyRunControl(string(provenance), pauseControlUpdate(true))
	if applied {
		f.mu.Lock()
		f.demoteControlBlockedReadyLocked()
		f.refillReadyLocked()
		f.mu.Unlock()
	}
	f.wake()

	return durable
}

func (f *Frontier) Resume(provenance []byte) {
	f.resumeControl(provenance)
}

func (f *Frontier) ResumeControl(provenance []byte) bool {
	return f.resumeControl(provenance)
}

func (f *Frontier) resumeControl(provenance []byte) bool {
	_, durable := f.applyRunControl(string(provenance), pauseControlUpdate(false))
	f.wake()

	return durable
}

func (f *Frontier) isPausedLocked(provenance []byte) bool {
	_, paused := f.paused[string(provenance)]

	return paused
}

func (f *Frontier) Cancel(provenance []byte) {
	f.cancelControl(provenance)
}

func (f *Frontier) CancelControl(provenance []byte) bool {
	return f.cancelControl(provenance)
}

func (f *Frontier) cancelControl(provenance []byte) bool {
	key := string(provenance)
	_, durable, finishes := f.applyRunCancellation(key)
	f.scheduleSettlements(finishes)
	f.wake()

	return durable
}

func (f *Frontier) WasCancelled(provenance []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, cancelled := f.state.cancelled[string(provenance)]

	return cancelled
}

func (f *Frontier) ClearCancelled(provenance []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := string(provenance)
	if f.cancelRuns[key] > 1 {
		f.cancelRuns[key]--

		return
	}
	delete(f.cancelRuns, key)
	delete(f.state.cancelled, key)
}

func (f *Frontier) SetRate(provenance []byte, pagesPerMinute uint32) {
	f.setRateControl(provenance, pagesPerMinute)
}

func (f *Frontier) SetRateControl(provenance []byte, pagesPerMinute uint32) bool {
	return f.setRateControl(provenance, pagesPerMinute)
}

func (f *Frontier) setRateControl(provenance []byte, pagesPerMinute uint32) bool {
	key := string(provenance)

	_, durable := f.applyRunControl(key, rateControlUpdate(pagesPerMinute))

	f.wake()

	return durable
}

func (f *Frontier) hasProvenanceLocked(provenance string) bool {
	for _, run := range f.state.runs {
		if run.provenance == provenance {
			return true
		}
	}

	return false
}

func (f *Frontier) rateIntervalLocked(key string) time.Duration {
	if interval, explicit := f.rateInterval[key]; explicit {
		return interval
	}

	return f.defaultRateInterval
}

func (f *Frontier) rateDueLocked(provenance []byte) (time.Time, bool) {
	if f.rateIntervalLocked(string(provenance)) <= 0 {
		return time.Time{}, false
	}

	return f.rateNextDue[string(provenance)], true
}

func (f *Frontier) recordRateVisitLocked(provenance []byte, at time.Time) {
	key := string(provenance)
	if interval := f.rateIntervalLocked(key); interval > 0 {
		f.rateNextDue[key] = at.Add(interval)
	}
}
