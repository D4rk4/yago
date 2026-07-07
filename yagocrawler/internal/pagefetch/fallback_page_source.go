package pagefetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type FallbackPageSource struct {
	primary  PageSource
	fallback PageSource
}

func NewFallbackPageSource(primary, fallback PageSource) *FallbackPageSource {
	return &FallbackPageSource{primary: primary, fallback: fallback}
}

func (s *FallbackPageSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (FetchedPage, error) {
	page, err := s.primary.Fetch(ctx, target)
	if err == nil {
		return page, nil
	}
	if !errors.Is(err, ErrPageRejected) {
		return FetchedPage{}, fmt.Errorf("primary fetch: %w", err)
	}
	// A non-HTML media type is refused by the same MIME policy in the browser, so
	// the fallback cannot rescue it — launching the browser would only waste a
	// tab (and, in a container, hit Chrome's sandbox failure) on every PDF or image.
	if errors.Is(err, ErrUnsupportedContentType) {
		return FetchedPage{}, fmt.Errorf("primary fetch: %w", err)
	}
	// A throttling status is the server asking for restraint: retrying the same
	// page through a full browser would hit the overloaded host harder, so the
	// throttle propagates for the politeness layer to back the host off.
	if _, throttled := AsThrottled(err); throttled {
		return FetchedPage{}, fmt.Errorf("primary fetch: %w", err)
	}
	// A permanently gone status (404/410) is the server's definitive verdict: the
	// browser would receive the same status, so it cannot rescue the page, and
	// escalating would only render the server's error page into a soft-404
	// document. The gone status propagates so the recrawl path can tombstone the
	// URL out of the index instead (ADR-0034), matching how mainstream crawlers
	// treat 404/410 as a removal signal rather than a render candidate.
	if _, gone := AsGone(err); gone {
		return FetchedPage{}, fmt.Errorf("primary fetch: %w", err)
	}
	if browserFallbackDisabled(ctx) {
		return FetchedPage{}, fmt.Errorf("primary fetch: %w", err)
	}

	page, err = s.fallback.Fetch(ctx, target)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("fallback fetch: %w", err)
	}
	return page, nil
}
