package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

const recrawlURL = "https://example.org/article"

var (
	issuedValidators = yagocrawlcontract.HTTPValidators{
		ETag:         `"v1"`,
		LastModified: "Wed, 01 Jul 2026 10:00:00 GMT",
	}
	updatedValidators = yagocrawlcontract.HTTPValidators{
		ETag:         `"v2"`,
		LastModified: "Wed, 01 Jul 2026 10:00:00 GMT",
	}
	issuedRevision = yagocrawlcontract.DocumentRevision{
		ContentHash:          "content-hash",
		ExtractionGeneration: 1,
		FetchedAt:            time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	recrawlObservedAt = time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
)

type recordedObservation struct {
	observation yagocrawlcontract.NotModifiedObservation
	envelope    ingest.Envelope
}

type recrawlEmitter struct {
	mu                sync.Mutex
	documents         []ingest.Envelope
	observations      []recordedObservation
	notModifiedResult func(context.Context) error
}

func (e *recrawlEmitter) Emit(
	_ context.Context,
	_ yagocrawlcontract.DocumentIngest,
	_ []yagomodel.RWIPosting,
	_ yagomodel.URIMetadataRow,
	envelope ingest.Envelope,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.documents = append(e.documents, envelope)

	return nil
}

func (*recrawlEmitter) EmitRemoval(context.Context, ingest.Envelope) error { return nil }

func (e *recrawlEmitter) EmitNotModified(
	ctx context.Context,
	observation yagocrawlcontract.NotModifiedObservation,
	envelope ingest.Envelope,
) error {
	e.mu.Lock()
	e.observations = append(
		e.observations,
		recordedObservation{observation: observation, envelope: envelope},
	)
	e.mu.Unlock()
	if e.notModifiedResult == nil {
		return nil
	}

	return e.notModifiedResult(ctx)
}

func (e *recrawlEmitter) recorded() ([]ingest.Envelope, []recordedObservation) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]ingest.Envelope(nil), e.documents...),
		append([]recordedObservation(nil), e.observations...)
}

type validatorAwareSource struct {
	mu       sync.Mutex
	requests []pagefetch.FetchRequest
	respond  func(pagefetch.FetchRequest) (pagefetch.FetchedPage, error)
}

func (s *validatorAwareSource) Fetch(
	_ context.Context,
	request pagefetch.FetchRequest,
) (pagefetch.FetchedPage, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()

	return s.respond(request)
}

func (s *validatorAwareSource) seen() []pagefetch.FetchRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]pagefetch.FetchRequest(nil), s.requests...)
}

func notModifiedWhenConditional(request pagefetch.FetchRequest) (pagefetch.FetchedPage, error) {
	if request.Validators != (yagocrawlcontract.HTTPValidators{}) {
		return pagefetch.FetchedPage{}, &pagefetch.NotModifiedError{Validators: updatedValidators}
	}

	return articlePage(request.Target), nil
}

func articlePage(target *url.URL) pagefetch.FetchedPage {
	return pagefetch.FetchedPage{
		URL:         target,
		HTTPStatus:  http.StatusOK,
		ContentType: "text/html",
		Body:        []byte(`<html><body><p>tide tables for the harbor piers</p></body></html>`),
		Validators:  updatedValidators,
	}
}

func conditionalRecrawlJob() crawljob.CrawlJob {
	return crawljob.CrawlJob{
		URL:           recrawlURL,
		ProfileHandle: "profile",
		Index:         true,
		ObservationID: "recrawl-observation",
		ObservedAt:    recrawlObservedAt,
		ConditionalFetch: &yagocrawlcontract.ConditionalFetch{
			Validators: issuedValidators,
			Revision:   issuedRevision,
		},
	}
}

func TestPipelineReportsAuthorizedNotModifiedObservation(t *testing.T) {
	frontier := newRecordingFrontier()
	source := &validatorAwareSource{respond: notModifiedWhenConditional}
	emitter := &recrawlEmitter{}
	observer := &countingPipelineObserver{}
	p := pipeline.NewPipeline(frontier, source, pageindex.NewIndexBuilder(), emitter,
		pipeline.WithNotModifiedEmitter(emitter), pipeline.WithObserver(observer))

	done := runJob(t, p, frontier, conditionalRecrawlJob())
	if done.httpStatus != http.StatusNotModified ||
		done.reason != "page not modified since the last fetch" ||
		done.outcome != (yagocrawlcontract.CrawlRunTally{Fetched: 1}) {
		t.Fatalf("done = %+v, want one unchanged fetch", done)
	}
	if requests := source.seen(); len(requests) != 1 || requests[0].Validators != issuedValidators {
		t.Fatalf("fetch requests = %+v, want one conditional request", requests)
	}
	documents, observations := emitter.recorded()
	if len(documents) != 0 || len(observations) != 1 {
		t.Fatalf("emitted %d documents and %d observations, want only one observation",
			len(documents), len(observations))
	}
	recorded := observations[0]
	if recorded.observation.Revision != issuedRevision ||
		recorded.envelope.SourceURL != recrawlURL ||
		recorded.envelope.ObservationID != "recrawl-observation" ||
		!recorded.envelope.ObservedAt.Equal(recrawlObservedAt) ||
		*recorded.envelope.ResponseValidators != updatedValidators {
		t.Fatalf("observation = %+v, want the issued revision with the job envelope", recorded)
	}
	if observer.fetchSucceeded != 1 || observer.ingestPublished != 1 || observer.fetchFailed != 0 {
		t.Fatalf("observer = %+v, want one successful published fetch", observer)
	}
}

func TestPipelineSendsValidatorsOnlyWithNotModifiedCapability(t *testing.T) {
	frontier := newRecordingFrontier()
	source := &validatorAwareSource{respond: notModifiedWhenConditional}
	emitter := &recrawlEmitter{}
	p := pipeline.NewPipeline(frontier, source, pageindex.NewIndexBuilder(), emitter)

	done := runJob(t, p, frontier, conditionalRecrawlJob())
	if requests := source.seen(); len(requests) != 1 ||
		requests[0].Validators != (yagocrawlcontract.HTTPValidators{}) {
		t.Fatalf("fetch requests = %+v, want one unconditional request", requests)
	}
	documents, observations := emitter.recorded()
	if done.httpStatus != http.StatusOK || len(documents) != 1 || len(observations) != 0 {
		t.Fatalf("done = %+v with %d documents and %d observations, want a full fetch",
			done, len(documents), len(observations))
	}
	if *documents[0].ResponseValidators != updatedValidators {
		t.Fatalf(
			"document validators = %+v, want the response validators",
			documents[0].ResponseValidators,
		)
	}
}

func TestPipelineFailsNotModifiedItDidNotRequest(t *testing.T) {
	unconditionalJob := conditionalRecrawlJob()
	unconditionalJob.ConditionalFetch = nil
	for _, tc := range []struct {
		name    string
		job     crawljob.CrawlJob
		options func(*recrawlEmitter) []pipeline.Option
	}{
		{
			name: "job without issued validators",
			job:  unconditionalJob,
			options: func(emitter *recrawlEmitter) []pipeline.Option {
				return []pipeline.Option{pipeline.WithNotModifiedEmitter(emitter)}
			},
		},
		{
			name:    "pipeline without not-modified capability",
			job:     conditionalRecrawlJob(),
			options: func(*recrawlEmitter) []pipeline.Option { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier := newRecordingFrontier()
			source := &validatorAwareSource{
				respond: func(pagefetch.FetchRequest) (pagefetch.FetchedPage, error) {
					return pagefetch.FetchedPage{}, &pagefetch.NotModifiedError{
						Validators: updatedValidators,
					}
				},
			}
			emitter := &recrawlEmitter{}
			p := pipeline.NewPipeline(
				frontier,
				source,
				pageindex.NewIndexBuilder(),
				emitter,
				tc.options(emitter)...)

			done := runJob(t, p, frontier, tc.job)
			documents, observations := emitter.recorded()
			if done.outcome.Failed != 1 || done.outcome.Fetched != 0 ||
				done.httpStatus == http.StatusNotModified ||
				len(documents) != 0 || len(observations) != 0 {
				t.Fatalf("done = %+v with %d documents and %d observations, want a failed fetch",
					done, len(documents), len(observations))
			}
		})
	}
}

func TestPipelineRefetchesWhenNodeDoesNotRecordNotModified(t *testing.T) {
	frontier := newRecordingFrontier()
	source := &validatorAwareSource{respond: notModifiedWhenConditional}
	emitter := &recrawlEmitter{notModifiedResult: func(context.Context) error {
		return fmt.Errorf("publish: %w", ingest.ErrNotModifiedUnrecorded)
	}}
	p := pipeline.NewPipeline(frontier, source, pageindex.NewIndexBuilder(), emitter,
		pipeline.WithNotModifiedEmitter(emitter))

	done := runJob(t, p, frontier, conditionalRecrawlJob())
	requests := source.seen()
	if len(requests) != 2 ||
		requests[0].Validators != issuedValidators ||
		requests[1].Validators != (yagocrawlcontract.HTTPValidators{}) {
		t.Fatalf("fetch requests = %+v, want conditional then unconditional", requests)
	}
	documents, observations := emitter.recorded()
	if done.httpStatus != http.StatusOK || done.outcome.Fetched != 1 || done.outcome.Failed != 0 ||
		len(documents) != 1 || len(observations) != 1 {
		t.Fatalf("done = %+v with %d documents and %d observations, want one full refetch",
			done, len(documents), len(observations))
	}
	if documents[0].ObservationID != "recrawl-observation" ||
		!documents[0].ObservedAt.Equal(recrawlObservedAt) {
		t.Fatalf("refetch envelope = %+v, want the job envelope", documents[0])
	}
}

func TestPipelineFailsNotModifiedWhenDeliveryFails(t *testing.T) {
	frontier := newRecordingFrontier()
	source := &validatorAwareSource{respond: notModifiedWhenConditional}
	emitter := &recrawlEmitter{notModifiedResult: func(context.Context) error {
		return errors.New("node unavailable")
	}}
	p := pipeline.NewPipeline(frontier, source, pageindex.NewIndexBuilder(), emitter,
		pipeline.WithNotModifiedEmitter(emitter))

	done := runJob(t, p, frontier, conditionalRecrawlJob())
	documents, _ := emitter.recorded()
	if done.reason != "not-modified observation delivery failed" ||
		done.outcome != (yagocrawlcontract.CrawlRunTally{Failed: 1}) ||
		len(source.seen()) != 1 || len(documents) != 0 {
		t.Fatalf("done = %+v after %d fetches and %d documents, want a delivery failure",
			done, len(source.seen()), len(documents))
	}
}

func TestPipelineAbandonsNotModifiedWhenItsLeaseEnds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result func(context.Context, context.CancelFunc) error
	}{
		{
			name: "lease lost",
			result: func(context.Context, context.CancelFunc) error {
				return fmt.Errorf("publish: %w", crawllease.ErrLeaseLost)
			},
		},
		{
			name: "worker cancelled",
			result: func(ctx context.Context, cancel context.CancelFunc) error {
				cancel()
				<-ctx.Done()

				return ctx.Err()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier := newRecordingFrontier()
			source := &validatorAwareSource{respond: notModifiedWhenConditional}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			emitter := &recrawlEmitter{notModifiedResult: func(emitContext context.Context) error {
				return tc.result(emitContext, cancel)
			}}
			p := pipeline.NewPipeline(frontier, source, pageindex.NewIndexBuilder(), emitter,
				pipeline.WithNotModifiedEmitter(emitter))
			go p.RunWorkers(ctx, ctx, 1)
			frontier.jobs <- conditionalRecrawlJob()
			select {
			case abandoned := <-frontier.abandoned:
				if abandoned.ObservationID != "recrawl-observation" {
					t.Fatalf("abandoned = %+v", abandoned)
				}
			case done := <-frontier.done:
				t.Fatalf("job completed as %+v, want abandonment", done)
			case <-time.After(5 * time.Second):
				t.Fatal("job neither completed nor was abandoned")
			}
		})
	}
}

func TestPipelineKeepsEmptyFullResponsesOnTheContentPath(t *testing.T) {
	emptyPage := func(request pagefetch.FetchRequest) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{
			URL:         request.Target,
			HTTPStatus:  http.StatusOK,
			ContentType: "text/html",
		}, nil
	}
	run := func(job crawljob.CrawlJob, options ...func(*recrawlEmitter) pipeline.Option) (doneCall, int, int) {
		frontier := newRecordingFrontier()
		emitter := &recrawlEmitter{}
		pipelineOptions := make([]pipeline.Option, 0, len(options))
		for _, option := range options {
			pipelineOptions = append(pipelineOptions, option(emitter))
		}
		p := pipeline.NewPipeline(frontier, &validatorAwareSource{respond: emptyPage},
			pageindex.NewIndexBuilder(), emitter, pipelineOptions...)
		done := runJob(t, p, frontier, job)
		documents, observations := emitter.recorded()

		return done, len(documents), len(observations)
	}
	plainJob := conditionalRecrawlJob()
	plainJob.ConditionalFetch = nil
	plain, plainDocuments, _ := run(plainJob)
	conditional, conditionalDocuments, observations := run(
		conditionalRecrawlJob(),
		func(emitter *recrawlEmitter) pipeline.Option { return pipeline.WithNotModifiedEmitter(emitter) },
	)
	if conditional.outcome != plain.outcome ||
		conditional.httpStatus != plain.httpStatus ||
		conditional.reason != plain.reason ||
		conditionalDocuments != plainDocuments ||
		observations != 0 {
		t.Fatalf("conditional empty response = %+v/%d/%d, want the plain outcome %+v/%d",
			conditional, conditionalDocuments, observations, plain, plainDocuments)
	}
}
