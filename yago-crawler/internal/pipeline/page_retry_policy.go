package pipeline

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

const (
	maximumPageRetryAttempts = 3
	basePageRetryDelay       = 5 * time.Second
	maximumPageRetryDelay    = 15 * time.Minute
)

func pageRetryDeadline(
	now time.Time,
	attempt uint8,
	serverDelay time.Duration,
) time.Time {
	delay := basePageRetryDelay
	for range attempt {
		if delay >= maximumPageRetryDelay/2 {
			delay = maximumPageRetryDelay
			break
		}
		delay *= 2
	}
	if delay < maximumPageRetryDelay {
		jitter, err := securePageRetryJitter(delay / 2)
		if err == nil {
			delay += jitter
		}
	}
	delay = max(delay, serverDelay)
	return now.Add(delay)
}

func retryableFetchFailure(
	ctx context.Context,
	err error,
	attempt uint8,
	now time.Time,
) (time.Time, bool) {
	if err == nil || ctx.Err() != nil || attempt >= maximumPageRetryAttempts {
		return time.Time{}, false
	}
	var throttled *pagefetch.ThrottledError
	if errors.As(err, &throttled) {
		return pageRetryDeadline(now, attempt, throttled.RetryAfter), true
	}
	if status, ok := pagefetch.AsHTTPStatus(err); ok {
		if status.Status == http.StatusRequestTimeout || status.Status == http.StatusTooEarly ||
			status.Status >= http.StatusInternalServerError {
			return pageRetryDeadline(now, attempt, 0), true
		}
		return time.Time{}, false
	}
	var networkError net.Error
	if errors.As(err, &networkError) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return pageRetryDeadline(now, attempt, 0), true
	}
	return time.Time{}, false
}
