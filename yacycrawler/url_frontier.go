package yacycrawler

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
)

const msgFrontierEnqueueFailed = "frontier enqueue failed"

type Frontier struct {
	jobs     JobSink
	finish   func()
	maxDepth int
	sameHost bool

	mu      sync.Mutex
	visited map[string]struct{}
	pending int
	closed  bool
}

func NewFrontier(jobs JobSink, finish func(), maxDepth int, sameHost bool) *Frontier {
	return &Frontier{
		jobs:     jobs,
		finish:   finish,
		maxDepth: maxDepth,
		sameHost: sameHost,
		visited:  make(map[string]struct{}),
	}
}

func (f *Frontier) Seed(ctx context.Context, seeds []string) {
	f.mu.Lock()
	f.pending++
	f.mu.Unlock()
	for _, seed := range seeds {
		if norm, ok := normalizeURL(seed); ok {
			f.accept(ctx, norm, 0)
		}
	}
	f.Done()
}

func (f *Frontier) Submit(ctx context.Context, fromURL string, links []string, depth int) {
	if depth >= f.maxDepth {
		return
	}
	base, err := url.Parse(fromURL)
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
		if f.sameHost && resolved.Host != base.Host {
			continue
		}
		if norm, ok := normalizeURL(resolved.String()); ok {
			f.accept(ctx, norm, depth+1)
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

func (f *Frontier) accept(ctx context.Context, normURL string, depth int) {
	f.mu.Lock()
	if _, seen := f.visited[normURL]; seen {
		f.mu.Unlock()
		return
	}
	f.visited[normURL] = struct{}{}
	f.pending++
	f.mu.Unlock()

	go func() {
		if err := f.jobs.Enqueue(ctx, CrawlJob{URL: normURL, Depth: depth}); err != nil {
			slog.Warn(msgFrontierEnqueueFailed, "url", normURL, "error", err)
			f.Done()
		}
	}()
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
