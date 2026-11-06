package httpfetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	ifNoneMatchHeader     = "If-None-Match"
	ifModifiedSinceHeader = "If-Modified-Since"
)

var errUnusableNotModified = errors.New(
	"origin answered not modified without a usable conditional request",
)

func (f *PageFetcher) Fetch(
	ctx context.Context,
	request pagefetch.FetchRequest,
) (pagefetch.FetchedPage, error) {
	started := time.Now()
	page, err := f.fetchRepresentation(ctx, request)
	if !errors.Is(err, errUnusableNotModified) {
		return page, err
	}
	retryContext, cancel := f.withinOriginalDeadline(ctx, started)
	defer cancel()

	return f.fetchRepresentation(retryContext, request.Unconditional())
}

func (f *PageFetcher) withinOriginalDeadline(
	ctx context.Context,
	started time.Time,
) (context.Context, context.CancelFunc) {
	if f.client.Timeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithDeadline(ctx, started.Add(f.client.Timeout))
}

func applyConditionalHeaders(header http.Header, validators yagocrawlcontract.HTTPValidators) {
	if validators.ETag != "" {
		header.Set(ifNoneMatchHeader, validators.ETag)
	}
	if validators.LastModified != "" {
		header.Set(ifModifiedSinceHeader, validators.LastModified)
	}
}

func RemoveConditionalHeaders(header http.Header) {
	header.Del(ifNoneMatchHeader)
	header.Del(ifModifiedSinceHeader)
}

func representationValidators(header http.Header) yagocrawlcontract.HTTPValidators {
	return yagocrawlcontract.HTTPValidators{
		ETag:         header.Get("ETag"),
		LastModified: header.Get("Last-Modified"),
	}
}

func notModifiedOutcome(
	issued yagocrawlcontract.HTTPValidators,
	response *http.Response,
) error {
	if !conditionalRequestSent(response.Request) {
		return errUnusableNotModified
	}
	updated := issued
	received := representationValidators(response.Header)
	if received.ETag != "" {
		updated.ETag = received.ETag
	}
	if received.LastModified != "" {
		updated.LastModified = received.LastModified
	}
	if err := updated.Validate(); err != nil {
		return fmt.Errorf("%w: %w", errUnusableNotModified, err)
	}

	return &pagefetch.NotModifiedError{Validators: updated}
}

func conditionalRequestSent(request *http.Request) bool {
	return request != nil &&
		(request.Header.Get(ifNoneMatchHeader) != "" ||
			request.Header.Get(ifModifiedSinceHeader) != "")
}
