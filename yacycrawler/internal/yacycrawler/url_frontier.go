package yacycrawler

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const msgFrontierEnqueueFailed = "frontier enqueue failed"

type Frontier struct {
	jobs     JobSink
	finish   func()
	registry *CrawlProfileRegistry

	mu        sync.Mutex
	visited   map[string]struct{}
	hostPages map[string]int
	pending   int
	closed    bool
}

func NewFrontier(jobs JobSink, finish func(), registry *CrawlProfileRegistry) *Frontier {
	return &Frontier{
		jobs:      jobs,
		finish:    finish,
		registry:  registry,
		visited:   make(map[string]struct{}),
		hostPages: make(map[string]int),
	}
}

func (f *Frontier) Hold() {
	f.mu.Lock()
	f.pending++
	f.mu.Unlock()
}

func (f *Frontier) Release() {
	f.Done()
}

func (f *Frontier) Seed(
	ctx context.Context,
	requests []yacycrawlcontract.CrawlRequest,
	provenance []byte,
) {
	f.Hold()
	for _, req := range requests {
		if norm, ok := normalizeURL(req.URL); ok {
			f.accept(
				ctx,
				norm,
				req.Depth,
				req.ProfileHandle,
				provenance,
				yacycrawlcontract.UnlimitedPagesPerHost,
			)
		}
	}
	f.Done()
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
				norm,
				work.Depth+1,
				work.ProfileHandle,
				work.Provenance,
				profile.MaxPagesPerHost,
			)
		}
	}
}

func (f *Frontier) Done() {
	f.mu.Lock()
	f.pending--
	finished := f.pending == 0 && !f.closed
	if finished {
		f.closed = true
	}
	f.mu.Unlock()
	if finished {
		f.finish()
	}
}

func (f *Frontier) accept(
	ctx context.Context,
	normURL string,
	depth int,
	profileHandle string,
	provenance []byte,
	maxPagesPerHost int,
) {
	host := hostOf(normURL)
	f.mu.Lock()
	if _, seen := f.visited[normURL]; seen {
		f.mu.Unlock()
		return
	}
	if maxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost &&
		f.hostPages[host] >= maxPagesPerHost {
		f.mu.Unlock()
		return
	}
	f.visited[normURL] = struct{}{}
	f.hostPages[host]++
	f.pending++
	f.mu.Unlock()

	go func() {
		job := CrawlJob{
			URL:           normURL,
			Depth:         depth,
			ProfileHandle: profileHandle,
			Provenance:    provenance,
		}
		if err := f.jobs.Enqueue(ctx, job); err != nil {
			slog.Warn(msgFrontierEnqueueFailed, "url", normURL, "error", err)
			f.Done()
		}
	}()
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
