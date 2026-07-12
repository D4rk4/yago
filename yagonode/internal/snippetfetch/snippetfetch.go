package snippetfetch

import (
	"context"
	"fmt"
	"time"
)

const (
	enrichLimit      = 3
	fetchConcurrency = 3
	enrichBudget     = 200 * time.Millisecond
)

// TextFetcher loads a page and returns its visible text; implementations must
// be egress-guarded and size-bounded (the node wires extractfetch).
type TextFetcher func(ctx context.Context, rawURL string) (string, error)

type Enricher struct {
	fetch TextFetcher
	cache *boundedTextCache
	slots chan struct{}
}

// NewEnricher builds an enricher over the given fetcher; a nil fetcher yields
// a nil enricher, which disables enrichment.
func NewEnricher(fetch TextFetcher) *Enricher {
	if fetch == nil {
		return nil
	}

	return &Enricher{
		fetch: fetch,
		cache: newBoundedTextCache(time.Now),
		slots: make(chan struct{}, fetchConcurrency),
	}
}

// pageText serves the page's extracted text from the cache or fetches it.
func (e *Enricher) pageText(ctx context.Context, rawURL string) (string, error) {
	if text, ok := e.cachedPageText(rawURL); ok {
		return text, nil
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return "", fmt.Errorf("snippet fetch admission: %w", ctx.Err())
	}
	if text, ok := e.cachedPageText(rawURL); ok {
		return text, nil
	}
	text, err := e.fetch(ctx, rawURL)
	if err != nil {
		return "", err
	}
	e.cache.put(rawURL, text)

	return text, nil
}

func (e *Enricher) cachedPageText(rawURL string) (string, bool) {
	return e.cache.get(rawURL)
}
