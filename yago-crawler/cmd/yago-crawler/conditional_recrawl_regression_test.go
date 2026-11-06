package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

const recrawlArticleEntityTag = `"article-v1"`

type recrawlOutcome struct {
	tally      yagocrawlcontract.CrawlRunTally
	httpStatus uint32
	reason     string
}

type recrawlFrontier struct {
	jobs chan crawljob.CrawlJob
	done chan recrawlOutcome
}

func (f *recrawlFrontier) Take(ctx context.Context) (crawljob.CrawlJob, bool) {
	select {
	case job := <-f.jobs:
		return job, true
	case <-ctx.Done():
		return crawljob.CrawlJob{}, false
	}
}

func (*recrawlFrontier) Submit(
	context.Context,
	crawljob.CrawlJob,
	crawljob.DiscoveredLinks,
) uint64 {
	return 0
}

func (f *recrawlFrontier) Done(_ crawljob.CrawlJob, tally yagocrawlcontract.CrawlRunTally) {
	f.done <- recrawlOutcome{tally: tally}
}

func (f *recrawlFrontier) DoneWithPageOutcome(
	_ crawljob.CrawlJob,
	tally yagocrawlcontract.CrawlRunTally,
	httpStatus uint32,
	reason string,
) {
	f.done <- recrawlOutcome{tally: tally, httpStatus: httpStatus, reason: reason}
}

func (*recrawlFrontier) Abandon(crawljob.CrawlJob) {}

func (*recrawlFrontier) ResolveRedirect(crawljob.CrawlJob, string) bool { return true }

type recordedNotModified struct {
	observation yagocrawlcontract.NotModifiedObservation
	envelope    ingest.Envelope
}

type recrawlEmitter struct {
	mu          sync.Mutex
	documents   []ingest.Envelope
	notModified []recordedNotModified
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
	_ context.Context,
	observation yagocrawlcontract.NotModifiedObservation,
	envelope ingest.Envelope,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notModified = append(
		e.notModified,
		recordedNotModified{observation: observation, envelope: envelope},
	)

	return nil
}

func (e *recrawlEmitter) recorded() ([]ingest.Envelope, []recordedNotModified) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]ingest.Envelope(nil), e.documents...),
		append([]recordedNotModified(nil), e.notModified...)
}

type countingIndexBuilder struct {
	inner  pageindex.IndexBuilder
	builds atomic.Int32
}

func (b *countingIndexBuilder) Build(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
) (pageindex.Artifacts, error) {
	b.builds.Add(1)
	artifacts, err := b.inner.Build(page, stats)
	if err != nil {
		return pageindex.Artifacts{}, fmt.Errorf("build index: %w", err)
	}

	return artifacts, nil
}

type bodyReadCountingTransport struct {
	inner http.RoundTripper
	reads atomic.Int64
}

func (t *bodyReadCountingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.inner.RoundTrip(request)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}
	response.Body = countedResponseBody{
		Closer: response.Body,
		read:   response.Body.Read,
		reads:  &t.reads,
	}

	return response, nil
}

type countedResponseBody struct {
	io.Closer
	read  func([]byte) (int, error)
	reads *atomic.Int64
}

func (b countedResponseBody) Read(buffer []byte) (int, error) {
	b.reads.Add(1)

	return b.read(buffer)
}

func startRecrawlPipeline(
	t *testing.T,
	source pagefetch.PageSource,
	builder pageindex.IndexBuilder,
	emitter *recrawlEmitter,
) func(crawljob.CrawlJob) recrawlOutcome {
	t.Helper()
	frontier := &recrawlFrontier{
		jobs: make(chan crawljob.CrawlJob, 1),
		done: make(chan recrawlOutcome, 1),
	}
	crawl := pipeline.NewPipeline(
		frontier,
		source,
		builder,
		emitter,
		pipeline.WithNotModifiedEmitter(emitter),
	)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go crawl.RunWorkers(ctx, ctx, 1)

	return func(job crawljob.CrawlJob) recrawlOutcome {
		frontier.jobs <- job
		select {
		case outcome := <-frontier.done:
			return outcome
		case <-time.After(5 * time.Second):
			t.Fatal("crawl job did not finish")

			return recrawlOutcome{}
		}
	}
}

func newRecrawlArticleServer(t *testing.T) *httptest.Server {
	t.Helper()
	article := "<html><head><title>Harbor tide tables</title></head><body><main><p>" +
		strings.Repeat(
			"Harbor tide tables list the morning and evening water levels for each pier. ",
			20,
		) +
		"</p></main></body></html>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == recrawlArticleEntityTag {
			w.WriteHeader(http.StatusNotModified)

			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", recrawlArticleEntityTag)
		w.Header().Set("Last-Modified", "Wed, 01 Jul 2026 10:00:00 GMT")
		_, _ = w.Write([]byte(article))
	}))
	t.Cleanup(server.Close)

	return server
}

func TestConditionalRecrawlReportsNotModifiedWithoutReadingOrParsing(t *testing.T) {
	server := newRecrawlArticleServer(t)
	transport := &bodyReadCountingTransport{inner: server.Client().Transport}
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: dynamicRedirectPolicy(newRedirectLimit(5)),
	}
	var browserCalls atomic.Int32
	browser := pageSourceFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
		browserCalls.Add(1)

		return pagefetch.FetchedPage{}, pagefetch.ErrPageRejected
	})
	source := pagefetch.NewBrowserFallbackPageSource(
		httpfetch.NewPageFetcher(client, "crawler/1.0", 1<<20),
		browser,
		browserRenderNeeded,
	)
	emitter := &recrawlEmitter{}
	builder := &countingIndexBuilder{inner: pageindex.NewIndexBuilder()}
	run := startRecrawlPipeline(t, source, builder, emitter)

	full := run(crawljob.CrawlJob{URL: server.URL + "/tides", ProfileHandle: "h", Index: true})
	documents, _ := emitter.recorded()
	if full.httpStatus != http.StatusOK || len(documents) != 1 || builder.builds.Load() != 1 {
		t.Fatalf(
			"full fetch = %+v, %d documents, %d builds",
			full,
			len(documents),
			builder.builds.Load(),
		)
	}
	issued := *documents[0].ResponseValidators
	if issued.ETag != recrawlArticleEntityTag || transport.reads.Load() == 0 {
		t.Fatalf("full fetch validators = %+v after %d body reads", issued, transport.reads.Load())
	}

	bodyReadsBefore := transport.reads.Load()
	revision := yagocrawlcontract.DocumentRevision{
		ContentHash:          "content-hash",
		ExtractionGeneration: 1,
		FetchedAt:            time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	observedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	recrawl := run(crawljob.CrawlJob{
		URL:           server.URL + "/tides",
		ProfileHandle: "h",
		Index:         true,
		ObservedAt:    observedAt,
		ConditionalFetch: &yagocrawlcontract.ConditionalFetch{
			Validators: issued,
			Revision:   revision,
		},
	})
	documents, notModified := emitter.recorded()
	if recrawl.httpStatus != http.StatusNotModified || recrawl.tally.Fetched != 1 ||
		recrawl.tally.Failed != 0 {
		t.Fatalf("recrawl outcome = %+v, want one unchanged fetch", recrawl)
	}
	if reads := transport.reads.Load() - bodyReadsBefore; reads != 0 {
		t.Fatalf("recrawl made %d body reads, want none", reads)
	}
	if builder.builds.Load() != 1 || len(documents) != 1 || browserCalls.Load() != 0 {
		t.Fatalf("recrawl built %d, emitted %d documents, browser %d; want no extraction",
			builder.builds.Load(), len(documents), browserCalls.Load())
	}
	if len(notModified) != 1 ||
		notModified[0].observation.Revision != revision ||
		!notModified[0].envelope.ObservedAt.Equal(observedAt) ||
		*notModified[0].envelope.ResponseValidators != issued {
		t.Fatalf(
			"not-modified observations = %+v, want one bound to the issued revision",
			notModified,
		)
	}
}
