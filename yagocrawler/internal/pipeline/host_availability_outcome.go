package pipeline

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

type hostFetchOutcomeRecorder interface {
	RecordHostFetchOutcome(context.Context, crawljob.CrawlJob, bool)
}

type permanentFetchRejection interface {
	Permanent() bool
}

func (p *Pipeline) recordHostFetchSuccess(ctx context.Context, job crawljob.CrawlJob) {
	recorder, ok := p.frontier.(hostFetchOutcomeRecorder)
	if ok {
		recorder.RecordHostFetchOutcome(ctx, job, false)
	}
	if p.loadFeedback != nil {
		p.loadFeedback.Succeeded(job.URL, time.Now())
	}
}

func (p *Pipeline) recordHostFetchError(
	ctx context.Context,
	job crawljob.CrawlJob,
	err error,
) bool {
	recorder, ok := p.frontier.(hostFetchOutcomeRecorder)
	failure, observed := hostAvailabilityOutcome(ctx, err)
	if ok && observed {
		recorder.RecordHostFetchOutcome(ctx, job, failure)
	}

	return observed && failure
}

func hostAvailabilityOutcome(ctx context.Context, err error) (bool, bool) {
	if ctx.Err() != nil {
		return false, false
	}
	if _, ok := pagefetch.AsThrottled(err); ok {
		return true, true
	}
	if _, ok := pagefetch.AsGone(err); ok ||
		errors.Is(err, pagefetch.ErrUnsupportedContentType) {
		return false, false
	}
	if statusError, ok := pagefetch.AsHTTPStatus(err); ok {
		failed := statusError.Status == http.StatusForbidden ||
			statusError.Status == http.StatusRequestTimeout ||
			statusError.Status >= http.StatusInternalServerError

		return failed, failed
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true, true
	}
	var permanent permanentFetchRejection
	if errors.As(err, &permanent) && permanent.Permanent() {
		return false, false
	}
	if errors.Is(err, pagefetch.ErrPageRejected) {
		return false, false
	}

	return false, false
}
