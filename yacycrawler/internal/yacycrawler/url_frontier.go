package yacycrawler

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	msgSeedURLRejected      = "seed url rejected"
	msgSubmitRunUnknown     = "links submitted for unknown run"
	msgSubmitProfileUnknown = "links submitted for unknown profile"
	msgJobURLUnparsable     = "crawl job url unparsable"
	msgLinkUnparsable       = "discovered link unparsable"
	msgAcceptProfileUnknown = "crawl job accepted for unknown profile"
	msgSeedProfileMismatch  = "seed profile handle does not match order"
)

type Frontier struct {
	jobs     chan CrawlJob
	commands chan frontierCommand
}

type crawlRun struct {
	visited   map[string]struct{}
	hostPages map[string]int
	pending   int
	finish    func()
	profiles  map[string]CompiledProfile
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
	profile    CompiledProfile
	finish     func()
	result     chan SeededRun
}

type SeededRun struct {
	RunID  uuid.UUID
	Queued int
}

type frontierSubmit struct {
	ctx   context.Context
	work  CrawlJob
	links []string
	done  chan struct{}
}

type frontierDone struct {
	runID uuid.UUID
	done  chan struct{}
}

func NewFrontier(capacity int) *Frontier {
	frontier := &Frontier{
		jobs:     make(chan CrawlJob, capacity),
		commands: make(chan frontierCommand),
	}
	go frontier.run()
	return frontier
}

func (f *Frontier) Jobs() <-chan CrawlJob {
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
	profile CompiledProfile,
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

func (f *Frontier) Submit(ctx context.Context, work CrawlJob, links []string) {
	done := make(chan struct{})
	f.commands <- frontierSubmit{ctx: ctx, work: work, links: links, done: done}
	<-done
}

func (f *Frontier) Done(work CrawlJob) {
	done := make(chan struct{})
	f.commands <- frontierDone{runID: work.RunID, done: done}
	<-done
}

func (f *Frontier) run() {
	runs := make(map[uuid.UUID]*crawlRun)
	var ready []CrawlJob
	closed := false

	for {
		var send chan CrawlJob
		var next CrawlJob
		if len(ready) > 0 {
			send = f.jobs
			next = ready[0]
		}

		select {
		case command := <-f.commands:
			var finished []func()
			closed, finished = f.handle(command, runs, &ready, closed)
			for _, finish := range finished {
				go finish()
			}
			if closed {
				close(f.jobs)
				return
			}
		case send <- next:
			ready = ready[1:]
		}
	}
}

func (f *Frontier) handle(
	command frontierCommand,
	runs map[uuid.UUID]*crawlRun,
	ready *[]CrawlJob,
	closed bool,
) (bool, []func()) {
	switch c := command.(type) {
	case frontierHold:
		frontierRun(runs, uuid.Nil, nil, CompiledProfile{}).pending++
		close(c.done)
		return closed, nil
	case frontierRelease:
		closed, finished := finishFrontierJob(runs, uuid.Nil, closed)
		close(c.done)
		return closed, finished
	case frontierSeedRun:
		c.result <- seedFrontierRun(runs, ready, c)
		return closed, nil
	case frontierSubmit:
		submitFrontierLinks(c.ctx, runs, ready, c.work, c.links)
		close(c.done)
		return closed, nil
	case frontierDone:
		closed, finished := finishFrontierJob(runs, c.runID, closed)
		close(c.done)
		return closed, finished
	default:
		return closed, nil
	}
}

func seedFrontierRun(
	runs map[uuid.UUID]*crawlRun,
	ready *[]CrawlJob,
	command frontierSeedRun,
) SeededRun {
	runID := uuid.New()
	run := frontierRun(runs, runID, command.finish, command.profile)
	run.pending++
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
		norm, ok := normalizeURL(req.URL)
		if !ok {
			slog.WarnContext(command.ctx, msgSeedURLRejected,
				slog.String("url", req.URL),
				slog.String("profileHandle", req.ProfileHandle),
			)
			continue
		}
		if acceptFrontierJob(command.ctx, runs, ready, runID, frontierCandidate{
			normURL:       norm,
			depth:         req.Depth,
			profileHandle: req.ProfileHandle,
			provenance:    command.provenance,
		}) {
			queued++
		}
	}
	_, finished := finishFrontierJob(runs, runID, false)
	for _, finish := range finished {
		go finish()
	}
	return SeededRun{RunID: runID, Queued: queued}
}

func submitFrontierLinks(
	ctx context.Context,
	runs map[uuid.UUID]*crawlRun,
	ready *[]CrawlJob,
	work CrawlJob,
	links []string,
) {
	run, ok := runs[work.RunID]
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
	profile := compiled.Profile
	if work.Depth >= profile.MaxDepth {
		return
	}
	base, err := url.Parse(work.URL)
	if err != nil {
		slog.WarnContext(ctx, msgJobURLUnparsable,
			slog.String("url", work.URL),
			slog.Any("error", err),
		)
		return
	}
	for _, link := range links {
		ref, err := url.Parse(link)
		if err != nil {
			slog.WarnContext(ctx, msgLinkUnparsable,
				slog.String("link", link),
				slog.String("base", work.URL),
				slog.Any("error", err),
			)
			continue
		}
		resolved := base.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			continue
		}
		if !scopeAllows(profile.Scope, base, resolved) {
			continue
		}
		if !profile.AllowQueryURLs && resolved.RawQuery != "" {
			continue
		}
		if !compiled.URLAllowed(resolved.String()) {
			continue
		}
		if norm, ok := normalizeURL(resolved.String()); ok {
			acceptFrontierJob(ctx, runs, ready, work.RunID, frontierCandidate{
				normURL:       norm,
				depth:         work.Depth + 1,
				profileHandle: work.ProfileHandle,
				provenance:    work.Provenance,
			})
		}
	}
}

func finishFrontierJob(
	runs map[uuid.UUID]*crawlRun,
	runID uuid.UUID,
	closed bool,
) (bool, []func()) {
	run := frontierRun(runs, runID, nil, CompiledProfile{})
	run.pending--
	finishedRun := run.pending == 0
	if finishedRun && runID != uuid.Nil {
		delete(runs, runID)
	}
	finishedAll := runID == uuid.Nil && finishedRun && !closed
	if finishedAll {
		closed = true
	}
	var finished []func()
	if finishedRun && run.finish != nil {
		finished = append(finished, run.finish)
	}
	return closed, finished
}

type frontierCandidate struct {
	normURL       string
	depth         int
	profileHandle string
	provenance    []byte
}

func acceptFrontierJob(
	ctx context.Context,
	runs map[uuid.UUID]*crawlRun,
	ready *[]CrawlJob,
	runID uuid.UUID,
	candidate frontierCandidate,
) bool {
	host := hostOf(candidate.normURL)
	run := frontierRun(runs, runID, nil, CompiledProfile{})
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
	run.pending++
	*ready = append(*ready, CrawlJob{
		URL:           candidate.normURL,
		Depth:         candidate.depth,
		ProfileHandle: candidate.profileHandle,
		Provenance:    candidate.provenance,
		RunID:         runID,
	})
	return true
}

func frontierRun(
	runs map[uuid.UUID]*crawlRun,
	id uuid.UUID,
	finish func(),
	profile CompiledProfile,
) *crawlRun {
	run, ok := runs[id]
	if ok {
		if profile.Profile.Handle != "" {
			run.profiles[profile.Profile.Handle] = profile
		}
		return run
	}
	run = &crawlRun{
		visited:   make(map[string]struct{}),
		hostPages: make(map[string]int),
		finish:    finish,
		profiles:  make(map[string]CompiledProfile),
	}
	if profile.Profile.Handle != "" {
		run.profiles[profile.Profile.Handle] = profile
	}
	runs[id] = run
	return run
}

func scopeAllows(scope yacycrawlcontract.CrawlScope, base, resolved *url.URL) bool {
	switch scope {
	case yacycrawlcontract.ScopeWide:
		return true
	case yacycrawlcontract.ScopeSubpath:
		return resolved.Host == base.Host && strings.HasPrefix(resolved.Path, basePath(base.Path))
	default:
		return resolved.Host == base.Host
	}
}

func basePath(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx+1]
	}
	return path
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func normalizeURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}
