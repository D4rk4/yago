package frontier

import (
	"context"
	"log/slog"

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
	jobs     chan crawljob.CrawlJob
	commands chan frontierCommand
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

type frontierCommand interface{}

type frontierHold struct {
	done chan struct{}
}

type frontierRelease struct {
	done chan struct{}
}

type frontierSeedRun struct {
	ctx        context.Context
	requests   []yacycrawlcontract.CrawlRequest
	provenance []byte
	profile    crawladmission.AdmissionProfile
	finish     func()
	result     chan SeededRun
}

type SeededRun struct {
	RunID  uuid.UUID
	Queued int
}

type frontierSubmit struct {
	ctx   context.Context
	work  crawljob.CrawlJob
	links []string
	done  chan struct{}
}

type frontierDone struct {
	runID uuid.UUID
	done  chan struct{}
}

func NewFrontier(capacity int) *Frontier {
	frontier := &Frontier{
		jobs:     make(chan crawljob.CrawlJob, capacity),
		commands: make(chan frontierCommand),
	}
	go frontier.run()
	return frontier
}

func (f *Frontier) Jobs() <-chan crawljob.CrawlJob {
	return f.jobs
}

func (f *Frontier) Hold() {
	done := make(chan struct{})
	f.commands <- frontierHold{done: done}
	<-done
}

func (f *Frontier) Release() {
	done := make(chan struct{})
	f.commands <- frontierRelease{done: done}
	<-done
}

func (f *Frontier) SeedRun(
	ctx context.Context,
	requests []yacycrawlcontract.CrawlRequest,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
	finish func(),
) SeededRun {
	result := make(chan SeededRun)
	f.commands <- frontierSeedRun{
		ctx:        ctx,
		requests:   requests,
		provenance: provenance,
		profile:    profile,
		finish:     finish,
		result:     result,
	}
	return <-result
}

func (f *Frontier) Submit(ctx context.Context, work crawljob.CrawlJob, links []string) {
	done := make(chan struct{})
	f.commands <- frontierSubmit{ctx: ctx, work: work, links: links, done: done}
	<-done
}

func (f *Frontier) Done(work crawljob.CrawlJob) {
	done := make(chan struct{})
	f.commands <- frontierDone{runID: work.RunID, done: done}
	<-done
}

func (f *Frontier) run() {
	state := &frontierState{
		runs:       make(map[uuid.UUID]*crawlRun),
		completion: crawlrun.NewCompletion(),
	}

	for {
		var send chan crawljob.CrawlJob
		var next crawljob.CrawlJob
		if len(state.ready) > 0 {
			send = f.jobs
			next = state.ready[0]
		}

		select {
		case command := <-f.commands:
			closeJobs, finished := f.handle(command, state)
			for _, finish := range finished {
				go finish()
			}
			if closeJobs {
				close(f.jobs)
				return
			}
		case send <- next:
			state.ready = state.ready[1:]
		}
	}
}

func (f *Frontier) handle(command frontierCommand, state *frontierState) (bool, []func()) {
	switch c := command.(type) {
	case frontierHold:
		state.completion.Hold()
		close(c.done)
		return false, nil
	case frontierRelease:
		drained := state.completion.Release()
		close(c.done)
		return drained, nil
	case frontierSeedRun:
		c.result <- state.seed(c)
		return false, nil
	case frontierSubmit:
		state.submit(c.ctx, c.work, c.links)
		close(c.done)
		return false, nil
	case frontierDone:
		finish, drained := state.completion.Settle(c.runID)
		close(c.done)
		if drained && finish != nil {
			return false, []func(){finish}
		}
		return false, nil
	default:
		return false, nil
	}
}

func (s *frontierState) seed(command frontierSeedRun) SeededRun {
	runID := uuid.New()
	s.runDedup(runID, command.profile)
	s.completion.Begin(runID, command.finish)
	queued := 0
	for _, req := range command.requests {
		if req.ProfileHandle != command.profile.Profile.Handle {
			slog.WarnContext(command.ctx, msgSeedProfileMismatch,
				slog.String("url", req.URL),
				slog.String("seedProfileHandle", req.ProfileHandle),
				slog.String("orderProfileHandle", command.profile.Profile.Handle),
			)
			continue
		}
		norm, ok := weburl.Normalize(req.URL)
		if !ok {
			slog.WarnContext(command.ctx, msgSeedURLRejected,
				slog.String("url", req.URL),
				slog.String("profileHandle", req.ProfileHandle),
			)
			continue
		}
		if s.accept(command.ctx, runID, frontierCandidate{
			normURL:       norm,
			depth:         req.Depth,
			profileHandle: req.ProfileHandle,
			provenance:    command.provenance,
		}) {
			queued++
		}
	}
	if finish, drained := s.completion.Settle(runID); drained && finish != nil {
		go finish()
	}
	return SeededRun{RunID: runID, Queued: queued}
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
