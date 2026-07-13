package frontier

import "time"

const (
	pendingControlLimit = 4096
	pendingControlTTL   = 10 * time.Minute
)

func (f *Frontier) retainPendingControlLocked(provenance string) {
	if f.hasProvenanceLocked(provenance) {
		delete(f.controlSeen, provenance)

		return
	}
	now := time.Now()
	f.purgePendingControlsLocked(now)
	f.controlSeen[provenance] = now
	if len(f.controlSeen) <= pendingControlLimit {
		return
	}
	oldestKey := ""
	oldest := now
	for key, seen := range f.controlSeen {
		if oldestKey == "" || seen.Before(oldest) {
			oldestKey = key
			oldest = seen
		}
	}
	f.discardPendingControlLocked(oldestKey)
}

func (f *Frontier) purgePendingControlsLocked(now time.Time) {
	for provenance, seen := range f.controlSeen {
		if now.Sub(seen) >= pendingControlTTL {
			f.discardPendingControlLocked(provenance)
		}
	}
}

func (f *Frontier) discardPendingControlLocked(provenance string) {
	delete(f.controlSeen, provenance)
	delete(f.paused, provenance)
	delete(f.state.cancelled, provenance)
	delete(f.rateInterval, provenance)
	delete(f.rateNextDue, provenance)
}
