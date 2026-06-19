package yacycrawler

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const msgFrontierEnqueueFailed = "frontier enqueue failed"

type Frontier struct {
	jobs     JobSink
	finish   func()
	registry *CrawlProfileRegistry

	mu     sync.Mutex
	runs   map[uuid.UUID]*crawlRun
	closed bool
}

type crawlRun struct {
	visited   map[string]struct{}
	hostPages map[string]int
	pending   int
	finish    func()
}

func NewFrontier(jobs JobSink, finish func(), registry *CrawlProfileRegistry) *Frontier {
	return &Frontier{
		jobs:     jobs,
		finish:   finish,
		registry: registry,
		runs:     make(map[uuid.UUID]*crawlRun),
	}
}

func (f *Frontier) Hold() {
	f.mu.Lock()
	f.run(uuid.Nil, nil).pending++
	f.mu.Unlock()
}

func (f *Frontier) Release() {
	f.done(uuid.Nil)
}

func (f *Frontier) SeedRun(
	ctx context.Context,
	requests []yacycrawlcontract.CrawlRequest,
	provenance []byte,
	finish func(),
) uuid.UUID {
	runID := uuid.New()
	f.mu.Lock()
	f.run(runID, finish).pending++
	f.mu.Unlock()
	for _, req := range requests {
		compiled, ok := f.registry.Lookup(req.ProfileHandle)
		if !ok {
			continue
		}
		if norm, ok := normalizeURL(req.URL); ok {
			f.accept(
				ctx,
				runID,
				norm,
				req.Depth,
				req.ProfileHandle,
				provenance,
				compiled.Profile.MaxPagesPerHost,
			)
		}
	}
	f.done(runID)
	return runID
}

func (f *Frontier) Submit(ctx context.Context, work CrawlJob, links []string) {
	compiled, ok := f.registry.Lookup(work.ProfileHandle)
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
			f.accept(
				ctx,
				work.RunID,
				norm,
				work.Depth+1,
				work.ProfileHandle,
				work.Provenance,
				profile.MaxPagesPerHost,
			)
		}
	}
}

func (f *Frontier) Done(work CrawlJob) {
	f.done(work.RunID)
}

func (f *Frontier) done(runID uuid.UUID) {
	f.mu.Lock()
	run := f.run(runID, nil)
	run.pending--
	finishedRun := run.pending == 0
	if finishedRun && runID != uuid.Nil {
		delete(f.runs, runID)
	}
	finishedAll := runID == uuid.Nil && finishedRun && !f.closed
	if finishedAll {
		f.closed = true
	}
	f.mu.Unlock()
	if finishedRun && run.finish != nil {
		run.finish()
	}
	if finishedAll {
		f.finish()
	}
}

func (f *Frontier) accept(
	ctx context.Context,
	runID uuid.UUID,
	normURL string,
	depth int,
	profileHandle string,
	provenance []byte,
	maxPagesPerHost int,
) {
	host := hostOf(normURL)
	f.mu.Lock()
	run := f.run(runID, nil)
	if _, seen := run.visited[normURL]; seen {
		f.mu.Unlock()
		return
	}
	if maxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost &&
		run.hostPages[host] >= maxPagesPerHost {
		f.mu.Unlock()
		return
	}
	run.visited[normURL] = struct{}{}
	run.hostPages[host]++
	run.pending++
	f.mu.Unlock()

	go func() {
		job := CrawlJob{
			URL:           normURL,
			Depth:         depth,
			ProfileHandle: profileHandle,
			Provenance:    provenance,
			RunID:         runID,
		}
		if err := f.jobs.Enqueue(ctx, job); err != nil {
			slog.Warn(msgFrontierEnqueueFailed, "url", normURL, "error", err)
			f.done(runID)
		}
	}()
}

func (f *Frontier) run(id uuid.UUID, finish func()) *crawlRun {
	run, ok := f.runs[id]
	if ok {
		return run
	}
	run = &crawlRun{
		visited:   make(map[string]struct{}),
		hostPages: make(map[string]int),
		finish:    finish,
	}
	f.runs[id] = run
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
