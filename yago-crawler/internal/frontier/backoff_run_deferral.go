package frontier

import (
	"time"

	"github.com/google/uuid"
)

type BackoffRunDeferral struct {
	Provenance []byte
	RetryAt    time.Time
}

type hostBackoffDeadline interface {
	BackoffUntil(rawURL string) time.Time
}

func (f *Frontier) DeferBackedOffRuns(now time.Time) []BackoffRunDeferral {
	pace, supportsBackoff := f.pace.(hostBackoffDeadline)
	if !supportsBackoff {
		return nil
	}
	f.mu.Lock()

	var deferred []BackoffRunDeferral
	var finishes []runFinish
	for runID, run := range f.state.runs {
		retryAt, eligible := f.backoffOnlyRunLocked(runID, run, now, pace)
		if !eligible {
			continue
		}
		provenance := append([]byte(nil), run.provenanceValue...)
		key := string(provenance)
		f.suspended[key] = struct{}{}
		f.deferredUntil[key] = retryAt
		finishes = append(finishes, f.cancelQueuedLocked(key)...)
		deferred = append(deferred, BackoffRunDeferral{
			Provenance: provenance,
			RetryAt:    retryAt,
		})
	}
	if len(deferred) > 0 {
		f.refillReadyLocked()
	}
	f.mu.Unlock()
	f.scheduleSettlements(finishes)
	if len(deferred) > 0 {
		f.wake()
	}

	return deferred
}

func (f *Frontier) backoffOnlyRunLocked(
	runID uuid.UUID,
	run *crawlRun,
	now time.Time,
	pace hostBackoffDeadline,
) (time.Time, bool) {
	if run == nil || run.pendingPages+f.readyPerRun[runID] == 0 || run.seeding ||
		run.awaitingDurability ||
		run.cancelled || run.seedRecovery || run.seedFinishing ||
		(run.boundedRecovery && !run.recoveryComplete) ||
		f.isPausedLocked(run.provenanceValue) {
		return time.Time{}, false
	}
	key := run.provenance
	if _, suspended := f.suspended[key]; suspended {
		return time.Time{}, false
	}
	ready := f.readyPerRun[runID]
	queued := run.pendingPages
	if f.state.completion.Pending(runID) != queued+ready {
		return time.Time{}, false
	}

	var earliest time.Time
	seen := 0
	for _, job := range f.state.ready {
		if job.RunID != runID {
			continue
		}
		seen++
		deadline := pace.BackoffUntil(job.URL)
		if !deadline.After(now) {
			return time.Time{}, false
		}
		earliest = earlierTime(earliest, deadline)
	}
	for _, bucket := range run.pendingHosts {
		if bucket == nil || !bucket.hasPages() {
			continue
		}
		seen += len(bucket.returned) - bucket.returnedHead +
			len(bucket.queued) - bucket.queuedHead
		deadline := pace.BackoffUntil(bucket.first().normURL)
		if !deadline.After(now) {
			return time.Time{}, false
		}
		earliest = earlierTime(earliest, deadline)
	}

	return earliest, seen == queued+ready && !earliest.IsZero()
}

func earlierTime(current, candidate time.Time) time.Time {
	if current.IsZero() || candidate.Before(current) {
		return candidate
	}

	return current
}

func (f *Frontier) DeferredUntil(provenance []byte) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	due, found := f.deferredUntil[string(provenance)]

	return due, found
}
