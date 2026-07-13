package frontier

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/crawlrun"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

const (
	msgSeedURLRejected        = "seed url rejected"
	msgSubmitRunUnknown       = "links submitted for unknown run"
	msgSubmitProfileUnknown   = "links submitted for unknown profile"
	msgAcceptProfileUnknown   = "crawl job accepted for unknown profile"
	msgSeedProfileMismatch    = "seed profile handle does not match order"
	msgRunPageBudgetReached   = "crawl run reached its page budget"
	frontierMutationBatchSize = 256
)

type Frontier struct {
	signal chan struct{}
	pace   CrawlPace

	maxPerHost int
	maxReady   int

	scorer       ValueScorer
	prepareSeeds func(
		context.Context,
		[]yagocrawlcontract.CrawlRequest,
		[]byte,
		crawladmission.AdmissionProfile,
	) []frontierCandidate
	prepareLinks func(
		crawljob.CrawlJob,
		crawljob.DiscoveredLinks,
		crawladmission.AdmissionProfile,
	) []frontierCandidate

	mu                sync.Mutex
	settlements       sync.WaitGroup
	state             *frontierState
	inflight          map[string]int
	paused            map[string]struct{}
	controlSeen       map[string]time.Time
	cancelRuns        map[string]int
	readyPerRun       map[uuid.UUID]int
	dispatchOrder     map[uuid.UUID]uint64
	nextDispatchOrder uint64
	readyOrder        map[uuid.UUID]uint64
	nextReadyOrder    uint64
	// rate throttles a run to a page budget: rateInterval holds the minimum gap
	// between the run's dispatches (an explicit zero entry lifts the throttle),
	// and rateNextDue the earliest time its next job may dispatch, both keyed by
	// provenance and layered on top of the per-host crawl delay. A run without
	// an explicit entry paces at defaultRateInterval (zero = no default).
	rateInterval        map[string]time.Duration
	rateNextDue         map[string]time.Time
	defaultRateInterval time.Duration
	closing             bool
}

// Option configures a Frontier at construction.
type Option func(*Frontier)

// WithMaxHostConcurrency bounds how many of a single host's URLs may be in flight
// (dispatched but not yet Done) at once, so a host whose fetches outlast the crawl
// delay cannot accumulate concurrent same-host fetches up to the worker count. A
// value <= 0 leaves per-host concurrency unbounded.
func WithMaxHostConcurrency(maxPerHost int) Option {
	return func(f *Frontier) { f.maxPerHost = maxPerHost }
}

// WithMaxPagesPerRun caps the total number of pages a single crawl run may admit
// across all hosts (a whole-run budget above the per-host cap), so a spider trap
// that mints an unbounded URL space cannot make one run crawl forever. A value
// <= 0 leaves the run budget unlimited.
func WithMaxPagesPerRun(maxPagesPerRun int) Option {
	return func(f *Frontier) { f.state.maxPagesPerRun = maxPagesPerRun }
}

// WithDefaultRunRate paces every run at pagesPerMinute dispatches from its
// first job, so a freshly started crawl is polite by default instead of
// running at full speed until an operator throttles it. An explicit SetRate
// overrides the default per run — including a rate of zero, which an operator
// uses to deliberately unleash a run. A value of zero disables the default.
func WithDefaultRunRate(pagesPerMinute uint32) Option {
	return func(f *Frontier) {
		if pagesPerMinute > 0 {
			f.defaultRateInterval = time.Minute / time.Duration(pagesPerMinute)
		}
	}
}

type frontierState struct {
	runs       map[uuid.UUID]*crawlRun
	completion *crawlrun.Completion
	ready      []crawljob.CrawlJob
	tally      RunTally
	// cancelled lives on the state, rather than beside paused on the Frontier, so
	// accept (a frontierState method) can reject a cancelled run's discovered links.
	cancelled map[string]struct{}
	// maxPagesPerRun caps how many pages one run may admit in total (0 = unlimited),
	// a whole-run ceiling above the per-host cap so a spider trap spanning many hosts
	// cannot crawl unbounded.
	maxPagesPerRun int
}

type crawlRun struct {
	visited         map[string]struct{}
	hostPages       map[string]int
	profiles        map[string]crawladmission.AdmissionProfile
	provenance      string
	provenanceValue []byte
	seeding         bool
	pendingByHost   map[string]*pendingHostPages
	pendingHosts    []*pendingHostPages
	pendingCursor   int
	pendingHostLive int
	pendingPages    int
	pages           int
	budgetExceeded  bool
	cancelled       bool
}

type SeededRun struct {
	RunID  uuid.UUID
	Queued int
}

func NewFrontier(capacity int, pace CrawlPace, opts ...Option) *Frontier {
	if pace == nil {
		pace = alwaysDuePace{}
	}
	frontier := &Frontier{
		signal:       make(chan struct{}, 1),
		scorer:       DefaultValueScorer,
		pace:         pace,
		maxReady:     min(max(1, capacity), maximumFrontierReadyJobs),
		prepareSeeds: prepareSeedCandidates,
		prepareLinks: prepareDiscoveredCandidates,
		state: &frontierState{
			runs:       make(map[uuid.UUID]*crawlRun),
			completion: crawlrun.NewCompletion(),
			tally:      noopRunTally{},
			cancelled:  make(map[string]struct{}),
		},
		inflight:      make(map[string]int),
		paused:        make(map[string]struct{}),
		controlSeen:   make(map[string]time.Time),
		cancelRuns:    make(map[string]int),
		readyPerRun:   make(map[uuid.UUID]int),
		dispatchOrder: make(map[uuid.UUID]uint64),
		readyOrder:    make(map[uuid.UUID]uint64),
		rateInterval:  make(map[string]time.Duration),
		rateNextDue:   make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(frontier)
	}
	return frontier
}

func (f *Frontier) Hold() {
	f.mu.Lock()
	f.state.completion.Hold()
	f.mu.Unlock()
}

func (f *Frontier) Release() {
	f.mu.Lock()
	if f.state.completion.Release() {
		f.closing = true
	}
	f.mu.Unlock()
	f.wake()
}

func (f *Frontier) SeedRun(
	ctx context.Context,
	requests []yagocrawlcontract.CrawlRequest,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
	finish func(succeeded bool),
) SeededRun {
	candidates := f.prepareSeeds(ctx, requests, provenance, profile)
	runID := uuid.New()

	f.mu.Lock()
	f.state.beginRun(runID, provenance, profile, finish)
	provenanceKey := string(provenance)
	delete(f.controlSeen, provenanceKey)
	if _, cancelled := f.state.cancelled[provenanceKey]; cancelled {
		f.state.runs[runID].cancelled = true
		f.cancelRuns[provenanceKey]++
	}
	f.mu.Unlock()

	queued := 0
	for start := 0; start < len(candidates); start += frontierMutationBatchSize {
		end := min(start+frontierMutationBatchSize, len(candidates))
		f.mu.Lock()
		for _, candidate := range candidates[start:end] {
			if f.acceptLocked(ctx, runID, candidate) {
				queued++
			}
		}
		f.rebalanceReadyLocked()
		f.mu.Unlock()
	}

	f.mu.Lock()
	f.state.runs[runID].seeding = false
	f.demoteControlBlockedReadyLocked()
	f.rebalanceReadyLocked()
	f.refillReadyLocked()
	settled, succeeded, drained := f.state.completion.Settle(runID)
	if drained {
		f.cleanupRunLocked(runID)
	}
	f.mu.Unlock()
	f.wake()
	if drained && settled != nil {
		f.scheduleSettlement(settled, succeeded)
	}

	return SeededRun{RunID: runID, Queued: queued}
}

func (f *Frontier) Submit(
	ctx context.Context,
	work crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
) {
	compiled, ok := f.submissionProfile(ctx, work)
	if !ok || work.Depth >= compiled.Profile.MaxDepth {
		return
	}
	candidates := f.prepareLinks(work, links, compiled)
	for start := 0; start < len(candidates); start += frontierMutationBatchSize {
		end := min(start+frontierMutationBatchSize, len(candidates))
		f.mu.Lock()
		for _, candidate := range candidates[start:end] {
			f.acceptLocked(ctx, work.RunID, candidate)
		}
		f.rebalanceReadyLocked()
		f.mu.Unlock()
		f.wake()
	}
}

func (f *Frontier) submissionProfile(
	ctx context.Context,
	work crawljob.CrawlJob,
) (crawladmission.AdmissionProfile, bool) {
	f.mu.Lock()
	run, runKnown := f.state.runs[work.RunID]
	var compiled crawladmission.AdmissionProfile
	profileKnown := false
	if runKnown {
		compiled, profileKnown = run.profiles[work.ProfileHandle]
	}
	f.mu.Unlock()
	if !runKnown {
		slog.WarnContext(ctx, msgSubmitRunUnknown, slog.String("runId", work.RunID.String()))

		return crawladmission.AdmissionProfile{}, false
	}
	if !profileKnown {
		slog.WarnContext(ctx, msgSubmitProfileUnknown,
			slog.String("runId", work.RunID.String()),
			slog.String("profileHandle", work.ProfileHandle),
		)

		return crawladmission.AdmissionProfile{}, false
	}

	return compiled, true
}

// RunPending reports a run's outstanding page count (queued plus in-flight), so a
// periodic progress report can carry a live queue depth rather than only the seed
// count. A drained or unknown run reports 0.
func (f *Frontier) RunPending(runID uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.state.completion.Pending(runID)
}

func (f *Frontier) Done(work crawljob.CrawlJob, deliveryFailed bool) {
	f.mu.Lock()
	f.releaseHost(work.URL)
	if deliveryFailed {
		f.state.completion.Fail(work.RunID)
	}
	finish, succeeded, drained := f.state.completion.Settle(work.RunID)
	if drained {
		f.cleanupRunLocked(work.RunID)
	}
	f.mu.Unlock()
	// Releasing a host slot may make a withheld same-host job dispatchable, so
	// nudge the run loop to re-evaluate rather than wait for the next signal.
	f.wake()
	if drained && finish != nil {
		f.scheduleSettlement(finish, succeeded)
	}
}

// acquireHost and releaseHost track in-flight fetches per host under f.mu so the
// dispatch loop can withhold a host whose concurrency cap is reached. Both are
// no-ops when per-host concurrency is unbounded.
func (f *Frontier) acquireHost(url string) {
	if f.maxPerHost <= 0 {
		return
	}
	f.inflight[weburl.Host(url)]++
}

func (f *Frontier) releaseHost(url string) {
	if f.maxPerHost <= 0 {
		return
	}
	host := weburl.Host(url)
	if f.inflight[host] <= 1 {
		delete(f.inflight, host)

		return
	}
	f.inflight[host]--
}

func (f *Frontier) hostAtCapacity(url string) bool {
	if f.maxPerHost <= 0 {
		return false
	}

	return f.inflight[weburl.Host(url)] >= f.maxPerHost
}

func (f *Frontier) wake() {
	select {
	case f.signal <- struct{}{}:
	default:
	}
}

// Pause withholds a run's pending jobs from dispatch without dropping them: the
// dispatch loop skips its ready jobs until the run is resumed, while in-flight
// fetches finish normally. A run is identified by its provenance token.
func (f *Frontier) Pause(provenance []byte) {
	f.mu.Lock()
	f.retainPendingControlLocked(string(provenance))
	f.paused[string(provenance)] = struct{}{}
	f.demoteControlBlockedReadyLocked()
	f.refillReadyLocked()
	f.mu.Unlock()
	f.wake()
}

// Resume lifts a pause and wakes the dispatch loop so the run's withheld jobs
// become dispatchable again.
func (f *Frontier) Resume(provenance []byte) {
	f.mu.Lock()
	delete(f.paused, string(provenance))
	f.mu.Unlock()
	f.wake()
}

// isPausedLocked reports whether a job's run is paused. It is called from the
// dispatch loop, which already holds f.mu.
func (f *Frontier) isPausedLocked(provenance []byte) bool {
	_, paused := f.paused[string(provenance)]

	return paused
}

// Cancel drops a run's pending jobs and stops it accepting newly discovered links,
// so the run drains once its in-flight fetches finish rather than crawling on. The
// run is identified by its provenance token; WasCancelled then lets the completion
// callback settle the order as cancelled instead of finished.
func (f *Frontier) Cancel(provenance []byte) {
	key := string(provenance)

	f.mu.Lock()
	f.retainPendingControlLocked(key)
	f.state.cancelled[key] = struct{}{}
	for _, run := range f.state.runs {
		if run.provenance == key && !run.cancelled {
			run.cancelled = true
			f.cancelRuns[key]++
		}
	}
	delete(f.paused, key)
	finishes := make([]runFinish, 0, len(f.state.runs))
	finishes = append(finishes, f.cancelQueuedLocked(key)...)
	f.mu.Unlock()

	f.scheduleSettlements(finishes)
	f.wake()
}

// WasCancelled reports whether a run has been cancelled, so its completion
// callback can settle the order as cancelled rather than finished.
func (f *Frontier) WasCancelled(provenance []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, cancelled := f.state.cancelled[string(provenance)]

	return cancelled
}

// ClearCancelled forgets a cancelled run once it has drained, bounding the set.
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

// SetRate throttles a run to at most pagesPerMinute dispatches, spacing its jobs
// by a fixed interval on top of the per-host crawl delay. A rate of zero lifts the
// throttle — including the default run rate — restoring the run to full speed.
func (f *Frontier) SetRate(provenance []byte, pagesPerMinute uint32) {
	key := string(provenance)

	f.mu.Lock()
	f.retainPendingControlLocked(key)
	if pagesPerMinute == 0 {
		// An explicit zero entry overrides the default rate; deleting it would
		// silently re-apply the default on the next dispatch.
		f.rateInterval[key] = 0
		delete(f.rateNextDue, key)
	} else {
		f.rateInterval[key] = time.Minute / time.Duration(pagesPerMinute)
	}
	f.mu.Unlock()

	f.wake()
}

func (f *Frontier) hasProvenanceLocked(provenance string) bool {
	for _, run := range f.state.runs {
		if run.provenance == provenance {
			return true
		}
	}

	return false
}

// rateIntervalLocked resolves a run's effective dispatch gap: its explicit
// SetRate entry when one exists (zero meaning deliberately unthrottled),
// otherwise the frontier-wide default. Callers hold f.mu.
func (f *Frontier) rateIntervalLocked(key string) time.Duration {
	if interval, explicit := f.rateInterval[key]; explicit {
		return interval
	}

	return f.defaultRateInterval
}

// rateDueLocked returns the earliest time a job may dispatch under its run's rate
// throttle, or the zero time when the run is unthrottled. Callers hold f.mu.
func (f *Frontier) rateDueLocked(provenance []byte) (time.Time, bool) {
	if f.rateIntervalLocked(string(provenance)) <= 0 {
		return time.Time{}, false
	}

	return f.rateNextDue[string(provenance)], true
}

// recordRateVisitLocked advances a throttled run's next-eligible dispatch time.
// Callers hold f.mu.
func (f *Frontier) recordRateVisitLocked(provenance []byte, at time.Time) {
	key := string(provenance)
	if interval := f.rateIntervalLocked(key); interval > 0 {
		f.rateNextDue[key] = at.Add(interval)
	}
}

func (f *Frontier) nextDue(now time.Time) (crawljob.CrawlJob, int, time.Duration, bool) {
	var soonest time.Duration
	bestIndex := -1
	bestScore := 0.0
	for i, job := range f.state.ready {
		due, eligible := f.jobDueLocked(job, now)
		if !eligible {
			continue
		}
		wait := due.Sub(now)
		if wait <= 0 {
			score := f.jobValueLocked(job)
			if f.preferReadyJobLocked(job, score, bestIndex, bestScore) {
				bestIndex, bestScore = i, score
			}

			continue
		}
		if soonest == 0 || wait < soonest {
			soonest = wait
		}
	}
	if bestIndex >= 0 {
		return f.state.ready[bestIndex], bestIndex, 0, true
	}

	return crawljob.CrawlJob{}, 0, soonest, false
}

func (f *Frontier) jobDueLocked(
	job crawljob.CrawlJob,
	now time.Time,
) (time.Time, bool) {
	if f.isPausedLocked(job.Provenance) || f.hostAtCapacity(job.URL) {
		return time.Time{}, false
	}
	run := f.state.runs[job.RunID]
	if run == nil || run.seeding {
		return time.Time{}, false
	}

	return f.dispatchDueLocked(job, now), true
}

func (f *Frontier) dispatchDueLocked(job crawljob.CrawlJob, now time.Time) time.Time {
	due := f.pace.DueAt(job, now)
	if rateDue, throttled := f.rateDueLocked(job.Provenance); throttled && rateDue.After(due) {
		due = rateDue
	}

	return due
}

func (f *Frontier) preferReadyJobLocked(
	job crawljob.CrawlJob,
	score float64,
	bestIndex int,
	bestScore float64,
) bool {
	if bestIndex < 0 {
		return true
	}
	order := f.dispatchOrder[job.RunID]
	bestOrder := f.dispatchOrder[f.state.ready[bestIndex].RunID]

	return order < bestOrder || order == bestOrder && score > bestScore
}

func (s *frontierState) beginRun(
	runID uuid.UUID,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
	finish func(succeeded bool),
) {
	s.runs[runID] = &crawlRun{
		visited:       make(map[string]struct{}),
		hostPages:     make(map[string]int),
		pendingByHost: make(map[string]*pendingHostPages),
		profiles: map[string]crawladmission.AdmissionProfile{
			profile.Profile.Handle: profile,
		},
		provenance:      string(provenance),
		provenanceValue: provenance,
		seeding:         true,
	}
	s.completion.Begin(runID, finish)
}

type frontierCandidate struct {
	normURL          string
	host             string
	depth            int
	profileHandle    string
	provenance       []byte
	sourceModifiedAt time.Time
	indexAllowed     bool
}

type pendingPage struct {
	normURL          string
	depth            int
	profileHandle    string
	sourceModifiedAt time.Time
	indexAllowed     bool
}

func (s *frontierState) accept(
	ctx context.Context,
	runID uuid.UUID,
	candidate frontierCandidate,
) bool {
	if _, cancelled := s.cancelled[string(candidate.provenance)]; cancelled {
		return false
	}
	run, runKnown := s.runs[runID]
	if !runKnown {
		slog.WarnContext(ctx, msgAcceptProfileUnknown,
			slog.String("url", candidate.normURL),
			slog.String("profileHandle", candidate.profileHandle),
		)

		return false
	}
	profile, ok := run.profiles[candidate.profileHandle]
	if !ok {
		slog.WarnContext(ctx, msgAcceptProfileUnknown,
			slog.String("url", candidate.normURL),
			slog.String("profileHandle", candidate.profileHandle),
		)
		return false
	}
	if _, seen := run.visited[candidate.normURL]; seen {
		s.tally.Duplicate(candidate.provenance)
		return false
	}
	if profile.Profile.MaxPagesPerHost != yagocrawlcontract.UnlimitedPagesPerHost &&
		run.hostPages[candidate.host] >= profile.Profile.MaxPagesPerHost {
		return false
	}
	if s.maxPagesPerRun > 0 && run.pages >= s.maxPagesPerRun {
		if !run.budgetExceeded {
			run.budgetExceeded = true
			slog.WarnContext(ctx, msgRunPageBudgetReached,
				slog.String("runId", runID.String()),
				slog.Int("maxPagesPerRun", s.maxPagesPerRun),
			)
		}

		return false
	}
	run.visited[candidate.normURL] = struct{}{}
	run.hostPages[candidate.host]++
	run.pages++
	s.completion.Track(runID)
	s.ready = append(s.ready, candidateJob(runID, candidate, profile))
	return true
}
