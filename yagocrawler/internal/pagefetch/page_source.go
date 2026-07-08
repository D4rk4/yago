package pagefetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

var ErrPageRejected = errors.New("page rejected")

// ErrUnsupportedContentType is the page rejection raised when a fetched document's
// media type is not one the crawler parses. It wraps ErrPageRejected, so callers
// that only care that a page was rejected keep matching it, while the browser
// fallback can single it out: the browser enforces the same MIME policy, so it
// cannot rescue non-HTML media and must not be launched for it.
var ErrUnsupportedContentType = fmt.Errorf("unsupported content type: %w", ErrPageRejected)

type FetchedPage struct {
	URL         *url.URL
	ContentType string
	Body        []byte
	// RobotsTag carries the response's X-Robots-Tag header verbatim so the
	// pipeline can honor header-level noindex/nofollow directives. The
	// headless-browser fetch path cannot observe response headers and leaves
	// it empty.
	RobotsTag string
}

type PageSource interface {
	Fetch(ctx context.Context, target *url.URL) (FetchedPage, error)
}
