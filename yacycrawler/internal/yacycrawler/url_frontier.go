package yacycrawler

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
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
	requests   []yacycrawlcontract.CrawlRequest
	provenance []byte
	profile    CompiledProfile
	finish     func()
	result     chan uuid.UUID
}

type frontierSubmit struct {
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
	requests []yacycrawlcontract.CrawlRequest,
	provenance []byte,
	profile CompiledProfile,
	finish func(),
) uuid.UUID {
	result := make(chan uuid.UUID)
	f.commands <- frontierSeedRun{
		requests:   requests,
		provenance: provenance,
		profile:    profile,
		finish:     finish,
		result:     result,
	}
	return <-result
}

func (f *Frontier) Submit(_ context.Context, work CrawlJob, links []string) {
	done := make(chan struct{})
	f.commands <- frontierSubmit{work: work, links: links, done: done}
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
		runID := seedFrontierRun(runs, ready, c)
		c.result <- runID
		return closed, nil
	case frontierSubmit:
		submitFrontierLinks(runs, ready, c.work, c.links)
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
) uuid.UUID {
	runID := uuid.New()
	run := frontierRun(runs, runID, command.finish, command.profile)
	run.pending++
	for _, req := range command.requests {
		if req.ProfileHandle != command.profile.Profile.Handle {
			continue
		}
		if norm, ok := normalizeURL(req.URL); ok {
			acceptFrontierJob(
				runs,
				ready,
				runID,
				norm,
				req.Depth,
				req.ProfileHandle,
				command.provenance,
			)
		}
	}
	_, finished := finishFrontierJob(runs, runID, false)
	for _, finish := range finished {
		go finish()
	}
	return runID
}

func submitFrontierLinks(
	runs map[uuid.UUID]*crawlRun,
	ready *[]CrawlJob,
	work CrawlJob,
	links []string,
) {
	run, ok := runs[work.RunID]
	if !ok {
		return
	}
	compiled, ok := run.profiles[work.ProfileHandle]
	if !ok {
		return
	}
	profile := compiled.Profile
	if work.Depth >= profile.MaxDepth {
		return
	}
	base, err := url.Parse(work.URL)
	if err != nil {
		return
	}
	for _, link := range links {
		ref, err := url.Parse(link)
		if err != nil {
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
			acceptFrontierJob(
				runs,
				ready,
				work.RunID,
				norm,
				work.Depth+1,
				work.ProfileHandle,
				work.Provenance,
			)
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

func acceptFrontierJob(
	runs map[uuid.UUID]*crawlRun,
	ready *[]CrawlJob,
	runID uuid.UUID,
	normURL string,
	depth int,
	profileHandle string,
	provenance []byte,
) {
	host := hostOf(normURL)
	run := frontierRun(runs, runID, nil, CompiledProfile{})
	profile, ok := run.profiles[profileHandle]
	if !ok {
		return
	}
	if _, seen := run.visited[normURL]; seen {
		return
	}
	if profile.Profile.MaxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost &&
		run.hostPages[host] >= profile.Profile.MaxPagesPerHost {
		return
	}
	run.visited[normURL] = struct{}{}
	run.hostPages[host]++
	run.pending++
	*ready = append(*ready, CrawlJob{
		URL:           normURL,
		Depth:         depth,
		ProfileHandle: profileHandle,
		Provenance:    provenance,
		RunID:         runID,
	})
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
