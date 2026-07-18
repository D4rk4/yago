package frontier

import (
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

func (f *Frontier) hasPendingReadyCandidateLocked() bool {
	for _, run := range f.state.runs {
		if runHasPending(run) &&
			!run.seeding &&
			!run.awaitingDurability &&
			!run.cancelled &&
			!f.isPausedLocked(run.provenanceValue) {
			return true
		}
	}

	return false
}

func (f *Frontier) rebuildReadyForDispatchLocked(now time.Time) time.Duration {
	f.demoteAllReadyLocked()
	excluded, earliest := f.futureRateRunsLocked(now)
	selectedHosts := make(map[string]int)
	f.fillReadyLocked(excluded, func(job crawljob.CrawlJob) bool {
		host := weburl.Host(job.URL)
		if f.maxPerHost > 0 && f.inflight[host]+selectedHosts[host] >= f.maxPerHost {
			return false
		}
		due := f.dispatchDueLocked(job, now)
		wait := due.Sub(now)
		if wait > 0 {
			earliest = earlierWait(earliest, wait)

			return false
		}
		selectedHosts[host]++

		return true
	})
	if len(f.state.ready) == 0 {
		f.fillReadyLocked(nil, nil)
	}

	return earliest
}

func (f *Frontier) futureRateRunsLocked(
	now time.Time,
) (map[uuid.UUID]struct{}, time.Duration) {
	excluded := make(map[uuid.UUID]struct{})
	var earliest time.Duration
	for runID, run := range f.state.runs {
		if !runHasPending(run) || run.seeding || run.awaitingDurability || run.cancelled ||
			f.isPausedLocked(run.provenanceValue) {
			continue
		}
		due, throttled := f.rateDueLocked(run.provenanceValue)
		if !throttled || !due.After(now) {
			continue
		}
		excluded[runID] = struct{}{}
		earliest = earlierWait(earliest, due.Sub(now))
	}

	return excluded, earliest
}

func earlierWait(current time.Duration, candidate time.Duration) time.Duration {
	if candidate <= 0 {
		return current
	}
	if current == 0 || candidate < current {
		return candidate
	}

	return current
}
