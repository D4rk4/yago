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
	msgSeedURLRejected      = "seed url rejected"
	msgSubmitRunUnknown     = "links submitted for unknown run"
	msgSubmitProfileUnknown = "links submitted for unknown profile"
	msgAcceptProfileUnknown = "crawl job accepted for unknown profile"
	msgSeedProfileMismatch  = "seed profile handle does not match order"
)

type Frontier struct {
	jobs   chan crawljob.CrawlJob
	signal chan struct{}
	pace   CrawlPace

	maxPerHost int

	mu       sync.Mutex
	state    *frontierState
	inflight map[string]int
	paused   map[string]struct{}
	// rate throttles a run to a page budget: rateInterval holds the minimum gap
	// between the run's dispatches (0 = unthrottled), and rateNextDue the earliest
	// time its next job may dispatch, both keyed by provenance and layered on top of
	// the per-host crawl delay.
	rateInterval map[string]time.Duration
	rateNextDue  map[string]time.Time
	closing      bool
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

type frontierState struct {
	runs       map[uuid.UUID]*crawlRun
	completion *crawlrun.Completion
	ready      []crawljob.CrawlJob
	tally      RunTally
	// cancelled lives on the state, rather than beside paused on the Frontier, so
	// accept (a frontierState method) can reject a cancelled run's discovered links.
	cancelled map[string]struct{}
}

type crawlRun struct {
	visited   map[string]struct{}
	hostPages map[string]int
	profiles  map[string]crawladmission.AdmissionProfile
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
		jobs:   make(chan crawljob.CrawlJob, capacity),
		signal: make(chan struct{}, 1),
		pace:   pace,
		state: &frontierState{
			runs:       make(map[uuid.UUID]*crawlRun),
			completion: crawlrun.NewCompletion(),
			tally:      noopRunTally{},
			cancelled:  make(map[string]struct{}),
		},
		inflight:     make(map[string]int),
		paused:       make(map[string]struct{}),
		rateInterval: make(map[string]time.Duration),
		rateNextDue:  make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(frontier)
	}
	go frontier.run()
	return frontier
}

func (f *Frontier) Jobs() <-chan crawljob.CrawlJob {
	return f.jobs
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
	finish func(),
) SeededRun {
	f.mu.Lock()
	seeded, settled := f.state.seed(ctx, requests, provenance, profile, finish)
	f.mu.Unlock()
	f.wake()
	if settled != nil {
		go settled()
	}
	return seeded
}

func (f *Frontier) Submit(
	ctx context.Context,
	work crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
) {
	f.mu.Lock()
	f.state.submit(ctx, work, links)
	f.mu.Unlock()
	f.wake()
}

func (f *Frontier) Done(work crawljob.CrawlJob) {
	f.mu.Lock()
	f.releaseHost(work.URL)
	finish, drained := f.state.completion.Settle(work.RunID)
	f.mu.Unlock()
	// Releasing a host slot may make a withheld same-host job dispatchable, so
	// nudge the run loop to re-evaluate rather than wait for the next signal.
	f.wake()
	if drained && finish != nil {
		go finish()
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
	f.paused[string(provenance)] = struct{}{}
	f.mu.Unlock()
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
	f.state.cancelled[key] = struct{}{}
	delete(f.paused, key)
	var finishes []func()
	kept := f.state.ready[:0]
	for _, job := range f.state.ready {
		if string(job.Provenance) != key {
			kept = append(kept, job)

			continue
		}
		if finish, drained := f.state.completion.Settle(job.RunID); drained && finish != nil {
			finishes = append(finishes, finish)
		}
	}
	f.state.ready = kept
	f.mu.Unlock()

	for _, finish := range finishes {
		go finish()
	}
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

	delete(f.state.cancelled, string(provenance))
}

// SetRate throttles a run to at most pagesPerMinute dispatches, spacing its jobs
// by a fixed interval on top of the per-host crawl delay. A rate of zero lifts the
// throttle, restoring the run to full speed.
func (f *Frontier) SetRate(provenance []byte, pagesPerMinute uint32) {
	key := string(provenance)

	f.mu.Lock()
	if pagesPerMinute == 0 {
		delete(f.rateInterval, key)
		delete(f.rateNextDue, key)
	} else {
		f.rateInterval[key] = time.Minute / time.Duration(pagesPerMinute)
	}
	f.mu.Unlock()

	f.wake()
}

// rateDueLocked returns the earliest time a job may dispatch under its run's rate
// throttle, or the zero time when the run is unthrottled. Callers hold f.mu.
func (f *Frontier) rateDueLocked(provenance []byte) (time.Time, bool) {
	if _, throttled := f.rateInterval[string(provenance)]; !throttled {
		return time.Time{}, false
	}

	return f.rateNextDue[string(provenance)], true
}

// recordRateVisitLocked advances a throttled run's next-eligible dispatch time.
// Callers hold f.mu.
func (f *Frontier) recordRateVisitLocked(provenance []byte, at time.Time) {
	key := string(provenance)
	if interval, throttled := f.rateInterval[key]; throttled {
		f.rateNextDue[key] = at.Add(interval)
	}
}

func (f *Frontier) run() {
	for {
		now := time.Now()
		f.mu.Lock()
		next, index, wait, due := f.nextDue(now)
		var send chan crawljob.CrawlJob
		if due {
			send = f.jobs
		}
		closeJobs := f.closing && len(f.state.ready) == 0
		f.mu.Unlock()

		if closeJobs {
			close(f.jobs)
			return
		}

		var wakeup <-chan time.Time
		var timer *time.Timer
		if !due && wait > 0 {
			timer = time.NewTimer(wait)
			wakeup = timer.C
		}

		select {
		case <-f.signal:
		case <-wakeup:
		case send <- next:
			f.mu.Lock()
			dispatchedAt := time.Now()
			f.pace.Visited(next, dispatchedAt)
			f.recordRateVisitLocked(next.Provenance, dispatchedAt)
			f.acquireHost(next.URL)
			f.state.ready = append(f.state.ready[:index], f.state.ready[index+1:]...)
			f.mu.Unlock()
		}

		if timer != nil {
			timer.Stop()
		}
	}
}

func (f *Frontier) nextDue(now time.Time) (crawljob.CrawlJob, int, time.Duration, bool) {
	var soonest time.Duration
	for i, job := range f.state.ready {
		if f.isPausedLocked(job.Provenance) {
			continue
		}
		if f.hostAtCapacity(job.URL) {
			continue
		}
		due := f.pace.DueAt(job, now)
		if rateDue, throttled := f.rateDueLocked(job.Provenance); throttled && rateDue.After(due) {
			due = rateDue
		}
		wait := due.Sub(now)
		if wait <= 0 {
			return job, i, 0, true
		}
		if soonest == 0 || wait < soonest {
			soonest = wait
		}
	}
	return crawljob.CrawlJob{}, 0, soonest, false
}

func (s *frontierState) seed(
	ctx context.Context,
	requests []yagocrawlcontract.CrawlRequest,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
	finish func(),
) (SeededRun, func()) {
	runID := uuid.New()
	s.runDedup(runID, profile)
	s.completion.Begin(runID, finish)
	queued := 0
	for _, req := range requests {
		if req.ProfileHandle != profile.Profile.Handle {
			slog.WarnContext(ctx, msgSeedProfileMismatch,
				slog.String("url", req.URL),
				slog.String("seedProfileHandle", req.ProfileHandle),
				slog.String("orderProfileHandle", profile.Profile.Handle),
			)
			continue
		}
		norm, ok := weburl.Normalize(req.URL)
		if !ok {
			slog.WarnContext(ctx, msgSeedURLRejected,
				slog.String("url", req.URL),
				slog.String("profileHandle", req.ProfileHandle),
			)
			continue
		}
		if s.accept(ctx, runID, frontierCandidate{
			normURL:       norm,
			depth:         req.Depth,
			profileHandle: req.ProfileHandle,
			provenance:    provenance,
		}) {
			queued++
		}
	}
	if finish, drained := s.completion.Settle(runID); drained && finish != nil {
		return SeededRun{RunID: runID, Queued: queued}, finish
	}
	return SeededRun{RunID: runID, Queued: queued}, nil
}

func (s *frontierState) submit(
	ctx context.Context,
	work crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
) {
	run, ok := s.runs[work.RunID]
	if !ok {
		slog.WarnContext(ctx, msgSubmitRunUnknown, slog.String("runId", work.RunID.String()))
		return
	}
	compiled, ok := run.profiles[work.ProfileHandle]
	if !ok {
		slog.WarnContext(ctx, msgSubmitProfileUnknown,
			slog.String("runId", work.RunID.String()),
			slog.String("profileHandle", work.ProfileHandle),
		)
		return
	}
	if work.Depth >= compiled.Profile.MaxDepth {
		return
	}
	for _, norm := range compiled.AdmitLinks(
		work.URL,
		links.ByPolicy(compiled.Profile.FollowNoFollowLinks),
	) {
		s.accept(ctx, work.RunID, frontierCandidate{
			normURL:       norm,
			depth:         work.Depth + 1,
			profileHandle: work.ProfileHandle,
			provenance:    work.Provenance,
		})
	}
}

type frontierCandidate struct {
	normURL       string
	depth         int
	profileHandle string
	provenance    []byte
}

func (s *frontierState) accept(
	ctx context.Context,
	runID uuid.UUID,
	candidate frontierCandidate,
) bool {
	if _, cancelled := s.cancelled[string(candidate.provenance)]; cancelled {
		return false
	}
	host := weburl.Host(candidate.normURL)
	run := s.runDedup(runID, crawladmission.AdmissionProfile{})
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
		run.hostPages[host] >= profile.Profile.MaxPagesPerHost {
		return false
	}
	run.visited[candidate.normURL] = struct{}{}
	run.hostPages[host]++
	s.completion.Track(runID)
	s.ready = append(s.ready, crawljob.CrawlJob{
		URL:                candidate.normURL,
		Depth:              candidate.depth,
		ProfileHandle:      candidate.profileHandle,
		Provenance:         candidate.provenance,
		RunID:              runID,
		Index:              profile.IndexAllowed(candidate.normURL),
		CrawlDelay:         profile.Profile.CrawlDelay,
		IgnoreTLSAuthority: profile.Profile.IgnoreTLSAuthority,
	})
	return true
}

func (s *frontierState) runDedup(id uuid.UUID, profile crawladmission.AdmissionProfile) *crawlRun {
	run, ok := s.runs[id]
	if !ok {
		run = &crawlRun{
			visited:   make(map[string]struct{}),
			hostPages: make(map[string]int),
			profiles:  make(map[string]crawladmission.AdmissionProfile),
		}
		s.runs[id] = run
	}
	if profile.Profile.Handle != "" {
		run.profiles[profile.Profile.Handle] = profile
	}
	return run
}
