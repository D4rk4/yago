package frontier

import (
	"bytes"
	"context"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

const maximumFrontierReadyJobs = 8192

type runFinish struct {
	finish    func(bool)
	succeeded bool
}

type returnedRunPages struct {
	hosts []string
	pages map[string][]pendingPage
}

func (f *Frontier) acceptLocked(
	ctx context.Context,
	runID uuid.UUID,
	candidate frontierCandidate,
) (bool, bool) {
	accepted, duplicate := f.state.accept(ctx, runID, candidate)
	if !accepted {
		return false, duplicate
	}
	if run := f.state.runs[runID]; run != nil {
		run.retainBoundedResidentPage(candidate.normURL)
	}
	newIndex := len(f.state.ready) - 1
	if newIndex < f.maxReady {
		f.readyPerRun[runID]++

		return true, false
	}
	clear(f.state.ready[newIndex:])
	f.state.ready = f.state.ready[:newIndex]
	f.appendPendingLocked(runID, candidate)

	return true, false
}

func (f *Frontier) appendPendingLocked(runID uuid.UUID, candidate frontierCandidate) {
	run := f.state.runs[runID]
	if run != nil {
		run.appendPending(candidate)
	}
}

func (f *Frontier) refillReadyLocked() {
	f.fillReadyLocked(nil, nil)
}

func (f *Frontier) fillReadyLocked(
	excluded map[uuid.UUID]struct{},
	accept func(crawljob.CrawlJob) bool,
) {
	buckets := f.pendingReadyBucketsLocked(excluded)
	f.sortReadyBucketsLocked(buckets)
	for ready := 0; ready < len(buckets); ready++ {
		for index := 0; index < len(buckets[ready]); index++ {
			if len(f.state.ready) >= f.maxReady {
				return
			}
			f.promotePendingRunLocked(buckets[ready][index], ready, buckets, accept)
		}
	}
}

func (f *Frontier) pendingReadyBucketsLocked(
	excluded map[uuid.UUID]struct{},
) [][]uuid.UUID {
	buckets := make([][]uuid.UUID, f.maxReady+1)
	for runID, run := range f.state.runs {
		if !runHasPending(run) {
			continue
		}
		if _, skip := excluded[runID]; skip {
			continue
		}
		if run.seeding || run.awaitingDurability || run.cancelled ||
			f.isPausedLocked([]byte(run.provenance)) {
			continue
		}
		ready := min(f.readyPerRun[runID], f.maxReady)
		buckets[ready] = append(buckets[ready], runID)
	}

	return buckets
}

func (f *Frontier) sortReadyBucketsLocked(buckets [][]uuid.UUID) {
	for _, bucket := range buckets {
		sort.Slice(bucket, func(first, second int) bool {
			firstOrder := f.readyOrder[bucket[first]]
			secondOrder := f.readyOrder[bucket[second]]
			if firstOrder != secondOrder {
				return firstOrder < secondOrder
			}

			return bytes.Compare(bucket[first][:], bucket[second][:]) < 0
		})
	}
}

func (f *Frontier) promotePendingRunLocked(
	runID uuid.UUID,
	ready int,
	buckets [][]uuid.UUID,
	accept func(crawljob.CrawlJob) bool,
) {
	run := f.state.runs[runID]
	candidate, ok := f.popPendingCandidateLocked(runID, run, accept)
	if !ok {
		return
	}
	profile := run.profiles[candidate.profileHandle]
	f.state.ready = append(f.state.ready, candidateJob(runID, candidate, profile, run.leaseID))
	f.readyPerRun[runID]++
	f.nextReadyOrder++
	f.readyOrder[runID] = f.nextReadyOrder
	if runHasPending(run) && ready+1 < len(buckets) {
		buckets[ready+1] = append(buckets[ready+1], runID)
	}
}

func (f *Frontier) popPendingCandidateLocked(
	runID uuid.UUID,
	run *crawlRun,
	accept func(crawljob.CrawlJob) bool,
) (frontierCandidate, bool) {
	host, pending, ok := run.popPending(func(host string, page pendingPage) bool {
		if accept == nil {
			return true
		}
		candidate := pendingCandidate(run, host, page)
		profile := run.profiles[candidate.profileHandle]

		return accept(candidateJob(runID, candidate, profile, run.leaseID))
	})
	if !ok {
		return frontierCandidate{}, false
	}

	return pendingCandidate(run, host, pending), true
}

func pendingCandidate(run *crawlRun, host string, pending pendingPage) frontierCandidate {
	return frontierCandidate{
		normURL:          pending.normURL,
		host:             host,
		depth:            pending.depth,
		profileHandle:    pending.profileHandle,
		provenance:       run.provenanceValue,
		sourceModifiedAt: pending.sourceModifiedAt,
		indexAllowed:     pending.indexAllowed,
		observationID:    pending.observationID,
		observedAt:       pending.observedAt,
	}
}

func popPendingPage(pages []pendingPage, head int) (pendingPage, []pendingPage, int) {
	pending := pages[head]
	clear(pages[head : head+1])
	head++
	live := len(pages) - head
	if live == 0 {
		return pending, nil, 0
	}
	if head >= frontierMutationBatchSize && live*4 <= cap(pages) {
		compacted := make([]pendingPage, live)
		copy(compacted, pages[head:])
		clear(pages)

		return pending, compacted, 0
	}
	if head >= frontierMutationBatchSize && head*2 >= len(pages) {
		return pending, slices.Delete(pages, 0, head), 0
	}

	return pending, pages, head
}

func runHasPending(run *crawlRun) bool {
	return run.pendingPages > 0
}

func (f *Frontier) rebalanceReadyLocked() {
	f.demoteAllReadyLocked()
	f.refillReadyLocked()
}

func (f *Frontier) demoteAllReadyLocked() {
	f.returnReadyJobsLocked(f.state.ready)
	clear(f.state.ready)
	f.state.ready = f.state.ready[:0]
	clear(f.readyPerRun)
}

func (f *Frontier) demoteControlBlockedReadyLocked() {
	kept := f.state.ready[:0]
	returned := make([]crawljob.CrawlJob, 0)
	clear(f.readyPerRun)
	for _, job := range f.state.ready {
		run := f.state.runs[job.RunID]
		if run != nil && !run.seeding && !run.awaitingDurability && !run.cancelled &&
			!f.isPausedLocked(job.Provenance) {
			kept = append(kept, job)
			f.readyPerRun[job.RunID]++
			continue
		}
		returned = append(returned, job)
	}
	f.returnReadyJobsLocked(returned)
	clear(f.state.ready[len(kept):])
	f.state.ready = kept
}

func (f *Frontier) returnReadyJobsLocked(jobs []crawljob.CrawlJob) {
	byRun := make(map[uuid.UUID]*returnedRunPages)
	for _, job := range jobs {
		candidate := candidateFromJob(job)
		returned := byRun[job.RunID]
		if returned == nil {
			returned = &returnedRunPages{pages: make(map[string][]pendingPage)}
			byRun[job.RunID] = returned
		}
		if _, exists := returned.pages[candidate.host]; !exists {
			returned.hosts = append(returned.hosts, candidate.host)
		}
		returned.pages[candidate.host] = append(
			returned.pages[candidate.host],
			pendingPageFromCandidate(candidate),
		)
	}
	for runID, returned := range byRun {
		run := f.state.runs[runID]
		for _, host := range returned.hosts {
			run.prependReturned(host, returned.pages[host])
		}
	}
}

func (f *Frontier) removeReadyAtLocked(index int) crawljob.CrawlJob {
	job := f.state.ready[index]
	f.state.ready = slices.Delete(f.state.ready, index, index+1)
	if f.readyPerRun[job.RunID] <= 1 {
		delete(f.readyPerRun, job.RunID)
	} else {
		f.readyPerRun[job.RunID]--
	}

	return job
}

func (f *Frontier) cancelQueuedLocked(provenance string) []runFinish {
	settled := make(map[uuid.UUID]int)
	kept := f.state.ready[:0]
	clear(f.readyPerRun)
	for _, job := range f.state.ready {
		if string(job.Provenance) == provenance {
			if run := f.state.runs[job.RunID]; run != nil {
				run.releaseBoundedResidentPage(job.URL)
			}
			settled[job.RunID]++

			continue
		}
		kept = append(kept, job)
		f.readyPerRun[job.RunID]++
	}
	clear(f.state.ready[len(kept):])
	f.state.ready = kept
	for runID, run := range f.state.runs {
		if run.provenance != provenance {
			continue
		}
		if run.boundedRecovery {
			run.releaseBoundedPendingPages()
		}
		settled[runID] += run.clearPending()
	}
	finishes := make([]runFinish, 0, len(settled))
	for runID, count := range settled {
		if finish := f.settleQueuedManyLocked(runID, count); finish != nil {
			finishes = append(finishes, *finish)
		}
	}
	f.refillReadyLocked()

	return finishes
}

func (f *Frontier) settleQueuedManyLocked(runID uuid.UUID, settled int) *runFinish {
	finish, succeeded, drained := f.state.completion.SettleMany(runID, settled)
	if drained {
		f.cleanupRunLocked(runID)
	}
	if !drained || finish == nil {
		return nil
	}

	return &runFinish{finish: finish, succeeded: succeeded}
}

func candidateJob(
	runID uuid.UUID,
	candidate frontierCandidate,
	profile crawladmission.AdmissionProfile,
	leaseID string,
) crawljob.CrawlJob {
	return crawljob.CrawlJob{
		URL:                      candidate.normURL,
		Depth:                    candidate.depth,
		ProfileHandle:            candidate.profileHandle,
		Provenance:               candidate.provenance,
		LeaseID:                  leaseID,
		RunID:                    runID,
		Index:                    candidate.indexAllowed,
		SourceModifiedAt:         candidate.sourceModifiedAt,
		ObservationID:            candidate.observationID,
		ObservedAt:               candidate.observedAt,
		CrawlDelay:               profile.Profile.CrawlDelay,
		IgnoreTLSAuthority:       profile.Profile.IgnoreTLSAuthority,
		IgnoreRobots:             profile.Profile.IgnoreRobots,
		DisableBrowser:           profile.Profile.DisableBrowser,
		FollowNoFollowLinks:      profile.Profile.FollowNoFollowLinks,
		NoindexCanonicalMismatch: profile.Profile.NoindexCanonicalMismatch,
		Formats:                  profile.Profile.Formats,
	}
}

func candidateFromJob(job crawljob.CrawlJob) frontierCandidate {
	return frontierCandidate{
		normURL:          job.URL,
		host:             weburl.Host(job.URL),
		depth:            job.Depth,
		profileHandle:    job.ProfileHandle,
		provenance:       job.Provenance,
		sourceModifiedAt: job.SourceModifiedAt,
		indexAllowed:     job.Index,
		observationID:    job.ObservationID,
		observedAt:       job.ObservedAt,
	}
}
