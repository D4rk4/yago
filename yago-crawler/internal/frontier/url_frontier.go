package frontier

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlrun"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
	"github.com/D4rk4/yago/yagocrawlcontract"
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

	mu                           sync.Mutex
	controlOrder                 sync.Mutex
	settlements                  sync.WaitGroup
	state                        *frontierState
	inflight                     map[string]int
	paused                       map[string]struct{}
	suspended                    map[string]struct{}
	controlSeen                  map[string]time.Time
	pendingControl               map[string]pendingControlUpdate
	cancelRuns                   map[string]int
	readyPerRun                  map[uuid.UUID]int
	dispatchOrder                map[uuid.UUID]uint64
	nextDispatchOrder            uint64
	readyOrder                   map[uuid.UUID]uint64
	nextReadyOrder               uint64
	prioritizeAutomaticDiscovery bool
	automaticDiscoveryBurst      int
	// rate throttles a run to a page budget: rateInterval holds the minimum gap
	// between the run's dispatches (an explicit zero entry lifts the throttle),
	// and rateNextDue the earliest time its next job may dispatch, both keyed by
	// provenance and layered on top of the per-host crawl delay. A run without
	// an explicit entry paces at defaultRateInterval (zero = no default).
	rateInterval          map[string]time.Duration
	rateNextDue           map[string]time.Time
	defaultRateInterval   time.Duration
	pagesPerMinute        map[string]uint32
	defaultPagesPerMinute uint32
	closing               bool
	leaseBindingChanges   chan struct{}
	checkpoint            Checkpoint
	checkpointFailure     error
	checkpointShutdown    func()
	growthAdmission       GrowthAdmission
	urlDenylist           URLDenylist
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
		f.defaultPagesPerMinute = pagesPerMinute
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
	durability             sync.Mutex
	awaitingDurability     bool
	visited                map[string]struct{}
	hostPages              map[string]int
	profiles               map[string]crawladmission.AdmissionProfile
	provenance             string
	provenanceValue        []byte
	leaseID                string
	seeding                bool
	pendingByHost          map[string]*pendingHostPages
	pendingHosts           []*pendingHostPages
	pendingCursor          int
	pendingHostLive        int
	pendingPages           int
	pages                  int
	maxPages               int
	budgetExceeded         bool
	cancelled              bool
	priority               yagocrawlcontract.CrawlOrderPriority
	hostFailures           map[string]uint8
	hostGenerations        map[string]uint64
	retiredHosts           map[string]struct{}
	residentHostReferences map[string]int
	redirects              map[string]redirectReservation
	pageHostProgress       map[string]stagedPageHostProgress
	seedingTally           yagocrawlcontract.CrawlRunTally
	boundedRecovery        bool
	recoveryCursor         uint64
	recoveryUpper          uint64
	recoveryComplete       bool
	recoveryLoading        bool
	seedRecovery           bool
	seedRecoveryCursor     uint64
	seedRecoveryLength     uint64
	seedFinishing          bool
	seedCancelling         bool
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
		inflight:                     make(map[string]int),
		paused:                       make(map[string]struct{}),
		suspended:                    make(map[string]struct{}),
		controlSeen:                  make(map[string]time.Time),
		pendingControl:               make(map[string]pendingControlUpdate),
		cancelRuns:                   make(map[string]int),
		readyPerRun:                  make(map[uuid.UUID]int),
		dispatchOrder:                make(map[uuid.UUID]uint64),
		readyOrder:                   make(map[uuid.UUID]uint64),
		rateInterval:                 make(map[string]time.Duration),
		rateNextDue:                  make(map[string]time.Time),
		pagesPerMinute:               make(map[string]uint32),
		prioritizeAutomaticDiscovery: true,
		leaseBindingChanges:          make(chan struct{}),
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

func (f *Frontier) seedRun(
	ctx context.Context,
	seed CrawlRunSeed,
	profile crawladmission.AdmissionProfile,
	finish func(succeeded bool),
) SeededRun {
	runID := uuid.New()
	preparation, err := f.prepareRunCheckpoint(ctx, seed, profile)
	if err != nil {
		f.RecordCheckpointFailure(err)
		f.scheduleSettlement(finish, false)

		return SeededRun{RunID: runID}
	}
	if preparation.persistent && preparation.snapshot.Completed {
		f.scheduleSettlement(finish, !preparation.snapshot.Failed)

		return SeededRun{RunID: runID}
	}
	f.activateSeedRun(runID, seed, profile, finish, preparation)
	result, err := f.admitPreparedRunSeeds(ctx, runID, seed, preparation)
	if err != nil {
		f.RecordCheckpointFailure(err)

		return SeededRun{RunID: runID}
	}
	if !result.continued {
		return SeededRun{RunID: runID, Queued: result.queued}
	}

	return f.finishPreparedRunSeeding(ctx, runID, seed, preparation, result)
}

func (f *Frontier) Submit(
	ctx context.Context,
	work crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
) uint64 {
	compiled, ok := f.submissionProfile(ctx, work)
	if !ok || work.Depth >= compiled.Profile.MaxDepth {
		return 0
	}
	candidates := f.prepareLinks(work, links, compiled)
	var duplicates uint64
	for start := 0; start < len(candidates); start += frontierMutationBatchSize {
		end := min(start+frontierMutationBatchSize, len(candidates))
		batchDuplicates, continued := f.submitCandidateBatch(
			ctx,
			work,
			candidates[start:end],
		)
		duplicates += batchDuplicates
		if !continued {
			return duplicates
		}
	}

	return duplicates
}

func (f *Frontier) submissionProfile(
	ctx context.Context,
	work crawljob.CrawlJob,
) (crawladmission.AdmissionProfile, bool) {
	f.mu.Lock()
	run, runKnown := f.state.runs[work.RunID]
	var compiled crawladmission.AdmissionProfile
	profileKnown := false
	if runKnown && runLeaseMatchesJob(run, work) {
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

	pending := f.state.completion.Pending(runID)
	if run := f.state.runs[runID]; run != nil && run.seedRecovery && pending > 0 {
		pending--
	}

	return pending
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

func (f *Frontier) nextDue(now time.Time) readySelection {
	var soonest time.Duration
	all := readyCandidate{index: -1}
	normal := readyCandidate{index: -1}
	automatic := readyCandidate{index: -1}
	for i, job := range f.state.ready {
		due, eligible := f.jobDueLocked(job, now)
		if !eligible {
			continue
		}
		wait := due.Sub(now)
		if wait <= 0 {
			score := f.jobValueLocked(job)
			all = f.preferredReadyCandidate(all, i, job, score)
			if f.automaticDiscoveryRunLocked(job.RunID) {
				automatic = f.preferredReadyCandidate(automatic, i, job, score)
			} else {
				normal = f.preferredReadyCandidate(normal, i, job, score)
			}

			continue
		}
		if soonest == 0 || wait < soonest {
			soonest = wait
		}
	}
	selected, contended := f.selectReadyCandidate(all, normal, automatic)
	if selected.index >= 0 {
		return readySelection{
			job:       f.state.ready[selected.index],
			index:     selected.index,
			due:       true,
			contended: contended,
		}
	}

	return readySelection{wait: soonest}
}

func (f *Frontier) jobDueLocked(
	job crawljob.CrawlJob,
	now time.Time,
) (time.Time, bool) {
	if f.isPausedLocked(job.Provenance) || f.hostAtCapacity(job.URL) {
		return time.Time{}, false
	}
	run := f.state.runs[job.RunID]
	if run == nil || !runLeaseMatchesJob(run, job) || run.seeding ||
		run.awaitingDurability || run.cancelled {
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

type frontierCandidate struct {
	normURL          string
	host             string
	depth            int
	profileHandle    string
	provenance       []byte
	sourceModifiedAt time.Time
	indexAllowed     bool
	observationID    string
	observedAt       time.Time
}

type pendingPage struct {
	normURL          string
	depth            int
	profileHandle    string
	sourceModifiedAt time.Time
	indexAllowed     bool
	observationID    string
	observedAt       time.Time
}

func (s *frontierState) accept(
	ctx context.Context,
	runID uuid.UUID,
	candidate frontierCandidate,
) (bool, bool) {
	if _, cancelled := s.cancelled[string(candidate.provenance)]; cancelled {
		return false, false
	}
	run, runKnown := s.runs[runID]
	if !runKnown {
		slog.WarnContext(ctx, msgAcceptProfileUnknown,
			slog.String("url", candidate.normURL),
			slog.String("profileHandle", candidate.profileHandle),
		)

		return false, false
	}
	if _, retired := run.retiredHosts[candidate.host]; retired {
		return false, false
	}
	profile, ok := run.profiles[candidate.profileHandle]
	if !ok {
		slog.WarnContext(ctx, msgAcceptProfileUnknown,
			slog.String("url", candidate.normURL),
			slog.String("profileHandle", candidate.profileHandle),
		)
		return false, false
	}
	if _, seen := run.visited[candidate.normURL]; seen {
		return false, true
	}
	if profile.Profile.MaxPagesPerHost != yagocrawlcontract.UnlimitedPagesPerHost &&
		run.hostPages[candidate.host] >= profile.Profile.MaxPagesPerHost {
		return false, false
	}
	if run.maxPages > 0 && run.pages >= run.maxPages {
		if !run.budgetExceeded {
			run.budgetExceeded = true
			slog.WarnContext(ctx, msgRunPageBudgetReached,
				slog.String("runId", runID.String()),
				slog.Int("maxPagesPerRun", run.maxPages),
			)
		}

		return false, false
	}
	run.visited[candidate.normURL] = struct{}{}
	run.hostPages[candidate.host]++
	run.pages++
	s.completion.Track(runID)
	s.ready = append(s.ready, candidateJob(runID, candidate, profile, run.leaseID))
	return true, false
}
