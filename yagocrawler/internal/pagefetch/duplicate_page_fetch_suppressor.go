package pagefetch

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sync"
)

type DuplicatePageFetchSuppressor struct {
	inner    PageSource
	mu       sync.Mutex
	inflight map[pageFetchIdentity]*pageFetchInFlight
}

type pageFetchIdentity struct {
	target                 string
	withoutBrowserFallback bool
}

type pageFetchInFlight struct {
	done         chan struct{}
	page         FetchedPage
	err          error
	participants int
}

func NewDuplicatePageFetchSuppressor(inner PageSource) *DuplicatePageFetchSuppressor {
	return &DuplicatePageFetchSuppressor{
		inner:    inner,
		inflight: make(map[pageFetchIdentity]*pageFetchInFlight),
	}
}

func (s *DuplicatePageFetchSuppressor) Fetch(
	ctx context.Context,
	target *url.URL,
) (FetchedPage, error) {
	if err := ctx.Err(); err != nil {
		return FetchedPage{}, fmt.Errorf("start duplicate page fetch: %w", err)
	}

	identity := pageFetchIdentity{
		target:                 target.String(),
		withoutBrowserFallback: browserFallbackDisabled(ctx),
	}
	s.mu.Lock()
	flight := s.inflight[identity]
	if flight != nil {
		flight.participants++
		s.mu.Unlock()

		select {
		case <-flight.done:
			return cloneFetchedPage(flight.page), flight.err
		case <-ctx.Done():
			return FetchedPage{}, fmt.Errorf("wait for duplicate page fetch: %w", ctx.Err())
		}
	}

	flight = &pageFetchInFlight{done: make(chan struct{}), participants: 1}
	s.inflight[identity] = flight
	s.mu.Unlock()

	page, err := s.inner.Fetch(ctx, target)
	s.mu.Lock()
	flight.page = page
	flight.err = err
	delete(s.inflight, identity)
	close(flight.done)
	s.mu.Unlock()

	return cloneFetchedPage(page), err
}

func cloneFetchedPage(page FetchedPage) FetchedPage {
	cloned := page
	cloned.Body = bytes.Clone(page.Body)
	if page.URL != nil {
		clonedURL := *page.URL
		cloned.URL = &clonedURL
	}

	return cloned
}
