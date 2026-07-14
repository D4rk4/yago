package websearch

import (
	"context"
	"errors"
	"log/slog"
)

const msgWebSearchEnginesUnavailable = "web-search engines unavailable"

const (
	webSearchFailureBackend     = "backend"
	webSearchFailureCanceled    = "canceled"
	webSearchFailureDeadline    = "deadline"
	webSearchFailureNone        = "none"
	webSearchFailureUnavailable = "unavailable"
)

var errWebSearchEnginesUnavailable = errors.New("web-search engines are unavailable")

func (p *DDGSProvider) reportUnavailable(ctx context.Context, err error) {
	p.mu.Lock()
	if p.unavailableReported {
		p.mu.Unlock()

		return
	}
	p.unavailableReported = true
	p.mu.Unlock()
	slog.WarnContext(
		ctx,
		msgWebSearchEnginesUnavailable,
		slog.String("reason", webSearchFailureReason(err)),
	)
}

func (p *DDGSProvider) markAvailable() {
	p.mu.Lock()
	p.unavailableReported = false
	p.mu.Unlock()
}

func webSearchFailureReason(err error) string {
	switch {
	case err == nil:
		return webSearchFailureNone
	case errors.Is(err, errWebSearchEnginesUnavailable):
		return webSearchFailureUnavailable
	case errors.Is(err, context.Canceled):
		return webSearchFailureCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return webSearchFailureDeadline
	default:
		return webSearchFailureBackend
	}
}
