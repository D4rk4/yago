package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
) (pagefetch.FetchedPage, bool, error) {
	fetched, err := p.fetchJob(ctx, job, target, outcome)
	notModified, isNotModified := pagefetch.AsNotModified(err)
	issued := p.issuedConditionalFetch(job)
	if !isNotModified || issued == nil {
		return fetched, false, err
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
		fetched, err = p.fetchJob(ctx, unconditionalCrawlJob(job), target, outcome)

		return fetched, false, err
	}
	if err != nil {
		return pagefetch.FetchedPage{}, true, fmt.Errorf(
			"%w: %w",
			errNotModifiedDeliveryFailure,
			err,
		)
	}
	outcome.Fetched++
	p.observer.FetchSucceeded(0)
	p.observer.IngestPublished()
	slog.DebugContext(ctx, msgPageNotModified, slog.String("url", job.URL))

	return pagefetch.FetchedPage{}, true, nil
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
