package frontier

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawladmission"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlrun"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/weburl"
)

const (
	msgSeedURLRejected      = "seed url rejected"
	msgSubmitRunUnknown     = "links submitted for unknown run"
	msgSubmitProfileUnknown = "links submitted for unknown profile"
	msgAcceptProfileUnknown = "crawl job accepted for unknown profile"
	msgSeedProfileMismatch  = "seed profile handle does not match order"
	msgSeedRunDuplicate     = "crawl run already active for order id"
	msgRunPageBudgetReached = "crawl run reached its page budget"
)

type Frontier struct {
	jobs   chan crawljob.CrawlJob
	signal chan struct{}
	pace   CrawlPace

	mu      sync.Mutex
	state   *frontierState
	closing bool
}

type frontierState struct {
	runs           map[uuid.UUID]*crawlRun
	completion     *crawlrun.Completion
	ready          *hostSchedule
	maxPagesPerRun int
}

type crawlRun struct {
	visited        map[string]struct{}
	hostPages      map[string]int
	profiles       map[string]crawladmission.AdmissionProfile
	pages          int
	budgetExceeded bool
}

type RunSeeds struct {
	RunID      uuid.UUID
	Requests   []yacycrawlcontract.CrawlRequest
	Provenance []byte
	Profile    crawladmission.AdmissionProfile
}

func NewFrontier(capacity int, pace CrawlPace, maxPagesPerRun int) *Frontier {
	if pace == nil {
		pace = alwaysDuePace{}
	}
	frontier := &Frontier{
		jobs:   make(chan crawljob.CrawlJob, capacity),
		signal: make(chan struct{}, 1),
		pace:   pace,
		state: &frontierState{
			runs:           make(map[uuid.UUID]*crawlRun),
			completion:     crawlrun.NewCompletion(),
			ready:          newHostSchedule(pace),
			maxPagesPerRun: maxPagesPerRun,
		},
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
	seeds RunSeeds,
	finish func(succeeded bool),
) (queued int, duplicate bool) {
	f.mu.Lock()
	queued, settled, duplicate := f.state.seed(ctx, seeds, finish)
	f.mu.Unlock()
	if duplicate {
		return 0, true
	}
	f.wake()
	if settled != nil {
		go settled()
	}
	return queued, false
}

func (f *Frontier) Submit(ctx context.Context, work crawljob.CrawlJob, links []string) {
	f.mu.Lock()
	f.state.submit(ctx, work, links)
	f.mu.Unlock()
	f.wake()
}

func (f *Frontier) Done(work crawljob.CrawlJob, deliveryFailed bool) {
	f.mu.Lock()
	if deliveryFailed {
		f.state.completion.Fail(work.RunID)
	}
	finish, succeeded, drained := f.state.completion.Settle(work.RunID)
	f.mu.Unlock()
	if drained && finish != nil {
		go finish(succeeded)
	}
}

func (f *Frontier) wake() {
	select {
	case f.signal <- struct{}{}:
	default:
	}
}

func (f *Frontier) run() {
	for {
		now := time.Now()
		f.mu.Lock()
		next, wait, due := f.state.ready.peek(now)
		var send chan crawljob.CrawlJob
		if due {
			send = f.jobs
		}
		closeJobs := f.closing && f.state.ready.len() == 0
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
			f.state.ready.dispatched(next, time.Now())
			f.mu.Unlock()
		}

		if timer != nil {
			timer.Stop()
		}
	}
}

func (s *frontierState) seed(
	ctx context.Context,
	seeds RunSeeds,
	finish func(succeeded bool),
) (queued int, settled func(), duplicate bool) {
	runID := seeds.RunID
	if _, active := s.runs[runID]; active {
		slog.WarnContext(ctx, msgSeedRunDuplicate, slog.String("runId", runID.String()))
		return 0, nil, true
	}
	s.runDedup(runID, seeds.Profile)
	s.completion.Begin(runID, finish)
	for _, req := range seeds.Requests {
		if req.ProfileHandle != seeds.Profile.Profile.Handle {
			slog.WarnContext(ctx, msgSeedProfileMismatch,
				slog.String("url", req.URL),
				slog.String("seedProfileHandle", req.ProfileHandle),
				slog.String("orderProfileHandle", seeds.Profile.Profile.Handle),
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
			provenance:    seeds.Provenance,
		}) {
			queued++
		}
	}
	if finish, succeeded, drained := s.completion.Settle(runID); drained && finish != nil {
		return queued, func() { finish(succeeded) }, false
	}
	return queued, nil, false
}

func (s *frontierState) submit(ctx context.Context, work crawljob.CrawlJob, links []string) {
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
	for _, norm := range compiled.AdmitLinks(work.URL, links) {
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
		return false
	}
	if profile.Profile.MaxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost &&
		run.hostPages[host] >= profile.Profile.MaxPagesPerHost {
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
	run.hostPages[host]++
	run.pages++
	s.completion.Track(runID)
	s.ready.push(crawljob.CrawlJob{
		URL:           candidate.normURL,
		Depth:         candidate.depth,
		ProfileHandle: candidate.profileHandle,
		Provenance:    candidate.provenance,
		RunID:         runID,
	}, time.Now())
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
