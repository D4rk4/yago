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
	runs       map[uuid.UUID]*crawlRun
	completion *crawlrun.Completion
	ready      []crawljob.CrawlJob
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

func NewFrontier(capacity int, pace CrawlPace) *Frontier {
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
	requests []yacycrawlcontract.CrawlRequest,
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

func (f *Frontier) Submit(ctx context.Context, work crawljob.CrawlJob, links []string) {
	f.mu.Lock()
	f.state.submit(ctx, work, links)
	f.mu.Unlock()
	f.wake()
}

func (f *Frontier) Done(work crawljob.CrawlJob) {
	f.mu.Lock()
	finish, drained := f.state.completion.Settle(work.RunID)
	f.mu.Unlock()
	if drained && finish != nil {
		go finish()
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
			f.pace.Visited(next, time.Now())
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
		wait := f.pace.DueAt(job, now).Sub(now)
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
	requests []yacycrawlcontract.CrawlRequest,
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
	run.visited[candidate.normURL] = struct{}{}
	run.hostPages[host]++
	s.completion.Track(runID)
	s.ready = append(s.ready, crawljob.CrawlJob{
		URL:           candidate.normURL,
		Depth:         candidate.depth,
		ProfileHandle: candidate.profileHandle,
		Provenance:    candidate.provenance,
		RunID:         runID,
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
