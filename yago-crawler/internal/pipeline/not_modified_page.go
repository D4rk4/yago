package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	msgPageNotModified               = "crawl page not modified"
	msgNotModifiedUnrecorded         = "crawl not-modified observation unrecorded by node"
	pageNotModifiedReason            = "page not modified since the last fetch"
	notModifiedDeliveryFailureReason = "not-modified observation delivery failed"
)

var errNotModifiedDeliveryFailure = errors.New(notModifiedDeliveryFailureReason)

type changedPageFetch struct {
	page      pagefetch.FetchedPage
	unchanged bool
}

func WithNotModifiedEmitter(emitter ingest.NotModifiedEmitter) Option {
	return func(p *Pipeline) {
		p.notModifiedEmitter = emitter
	}
}

func (p *Pipeline) issuedConditionalFetch(
	job crawljob.CrawlJob,
) *yagocrawlcontract.ConditionalFetch {
	if p.notModifiedEmitter == nil {
		return nil
	}

	return job.ConditionalFetch
}

func (p *Pipeline) fetchRequest(job crawljob.CrawlJob, target *url.URL) pagefetch.FetchRequest {
	request := pagefetch.FetchRequest{Target: target}
	if issued := p.issuedConditionalFetch(job); issued != nil {
		request.Validators = issued.Validators
	}

	return request
}

func (p *Pipeline) fetchChangedPage(
	ctx context.Context,
	job crawljob.CrawlJob,
	target *url.URL,
	outcome *yagocrawlcontract.CrawlRunTally,
) (changedPageFetch, error) {
	fetched, err := p.fetchJob(ctx, job, target, outcome)
	notModified, isNotModified := pagefetch.AsNotModified(err)
	issued := p.issuedConditionalFetch(job)
	if !isNotModified || issued == nil {
		return changedPageFetch{page: fetched}, err
	}
	p.recordHostFetchSuccess(ctx, job)
	err = p.notModifiedEmitter.EmitNotModified(
		ctx,
		yagocrawlcontract.NotModifiedObservation{Revision: issued.Revision},
		ingest.Envelope{
			SourceURL:          job.URL,
			Provenance:         job.Provenance,
			ProfileHandle:      job.ProfileHandle,
			ObservationID:      job.ObservationID,
			ObservedAt:         job.ObservedAt,
			SourceModifiedAt:   job.SourceModifiedAt,
			ResponseValidators: &notModified.Validators,
		},
	)
	if errors.Is(err, ingest.ErrNotModifiedUnrecorded) {
		slog.WarnContext(ctx, msgNotModifiedUnrecorded, slog.String("url", job.URL))
		observeNotModifiedRefetch(p.observer)
		fetched, err = p.fetchJob(ctx, unconditionalCrawlJob(job), target, outcome)

		return changedPageFetch{page: fetched}, err
	}
	if err != nil {
		return changedPageFetch{unchanged: true}, fmt.Errorf(
			"%w: %w",
			errNotModifiedDeliveryFailure,
			err,
		)
	}
	outcome.Fetched++
	p.observer.FetchSucceeded(0)
	p.observer.IngestPublished()
	slog.DebugContext(ctx, msgPageNotModified, slog.String("url", job.URL))

	return changedPageFetch{unchanged: true}, nil
}

func (p *Pipeline) settlePageOutcome(
	ctx context.Context,
	job crawljob.CrawlJob,
	fetch changedPageFetch,
	fetchErr error,
	outcome *yagocrawlcontract.CrawlRunTally,
) (uint32, string, error) {
	if fetch.unchanged {
		return http.StatusNotModified, notModifiedPageReason(fetchErr), fetchErr
	}
	reason, err := p.processFetchedPage(ctx, job, fetch.page, outcome)

	return fetch.page.HTTPStatus, reason, err
}

func unconditionalCrawlJob(job crawljob.CrawlJob) crawljob.CrawlJob {
	job.ConditionalFetch = nil

	return job
}

func notModifiedPageReason(err error) string {
	if err != nil {
		return notModifiedDeliveryFailureReason
	}

	return pageNotModifiedReason
}
