package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type doneCall struct {
	work       crawljob.CrawlJob
	outcome    yagocrawlcontract.CrawlRunTally
	httpStatus uint32
	reason     string
}

type recordingFrontier struct {
	jobs       chan crawljob.CrawlJob
	submitted  []crawljob.DiscoveredLinks
	submitters []crawljob.CrawlJob
	done       chan doneCall
	abandoned  chan crawljob.CrawlJob
	redirects  []string
	duplicates uint64
	// resolveRedirect overrides the ResolveRedirect outcome; nil admits every
	// redirect target.
	resolveRedirect func(job crawljob.CrawlJob, finalURL string) bool
}

func newRecordingFrontier() *recordingFrontier {
	return &recordingFrontier{
		jobs:      make(chan crawljob.CrawlJob, 1),
		done:      make(chan doneCall, 8),
		abandoned: make(chan crawljob.CrawlJob, 8),
	}
}

func (f *recordingFrontier) Take(ctx context.Context) (crawljob.CrawlJob, bool) {
	select {
	case job, ok := <-f.jobs:
		return job, ok
	case <-ctx.Done():
		return crawljob.CrawlJob{}, false
	}
}

func (f *recordingFrontier) Submit(
	_ context.Context,
	work crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
) uint64 {
	f.submitted = append(f.submitted, links)
	f.submitters = append(f.submitters, work)

	return f.duplicates
}

func TestPipelineThreadsLeaseIntoDiscoveredLinkAdmission(t *testing.T) {
	frontier := newRecordingFrontier()
	crawlerPipeline := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
				return htmlPage(), nil
			},
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(
				context.Context,
				yagocrawlcontract.DocumentIngest,
				[]yagomodel.RWIPosting,
				yagomodel.URIMetadataRow,
				ingest.Envelope,
			) error {
				return nil
			},
		),
	)
	job := crawljob.CrawlJob{
		URL:           "https://example.com/",
		ProfileHandle: "lease-threading",
		LeaseID:       "lease-threading",
		Index:         true,
	}
	runJob(t, crawlerPipeline, frontier, job)
	if len(frontier.submitters) != 1 || frontier.submitters[0].LeaseID != job.LeaseID {
		t.Fatalf("discovered link admission lease = %+v", frontier.submitters)
	}
}

func (f *recordingFrontier) Done(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
) {
	f.done <- doneCall{work: work, outcome: outcome}
}

func (f *recordingFrontier) DoneWithReason(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	reason string,
) {
	f.done <- doneCall{work: work, outcome: outcome, reason: reason}
}

func (f *recordingFrontier) DoneWithPageOutcome(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	httpStatus uint32,
	reason string,
) {
	f.done <- doneCall{
		work: work, outcome: outcome, httpStatus: httpStatus, reason: reason,
	}
}

func (f *recordingFrontier) Abandon(work crawljob.CrawlJob) {
	f.abandoned <- work
}

func (f *recordingFrontier) ResolveRedirect(job crawljob.CrawlJob, finalURL string) bool {
	f.redirects = append(f.redirects, finalURL)
	if f.resolveRedirect == nil {
		return true
	}

	return f.resolveRedirect(job, finalURL)
}

func (*recordingFrontier) RecordHostFetchOutcome(
	context.Context,
	crawljob.CrawlJob,
	bool,
) {
}

type fetchFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f fetchFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

type indexFunc func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error)

func (f indexFunc) Build(
	p pageparse.ParsedPage,
	s pageparse.PageStats,
) (pageindex.Artifacts, error) {
	return f(p, s)
}

type emitFunc func(
	context.Context,
	yagocrawlcontract.DocumentIngest,
	[]yagomodel.RWIPosting,
	yagomodel.URIMetadataRow,
	ingest.Envelope,
) error

func (f emitFunc) Emit(
	ctx context.Context,
	document yagocrawlcontract.DocumentIngest,
	postings []yagomodel.RWIPosting,
	metadata yagomodel.URIMetadataRow,
	envelope ingest.Envelope,
) error {
	return f(ctx, document, postings, metadata, envelope)
}

func (emitFunc) EmitRemoval(context.Context, ingest.Envelope) error { return nil }

func htmlPage() pagefetch.FetchedPage {
	target, _ := url.Parse("https://example.com/")
	return pagefetch.FetchedPage{
		URL:         target,
		HTTPStatus:  200,
		ContentType: "text/html",
		Body:        []byte(`<html><body><a href="/next">go</a> words here</body></html>`),
	}
}

func htmlPageWithNoFollow() pagefetch.FetchedPage {
	target, _ := url.Parse("https://example.com/")
	return pagefetch.FetchedPage{
		URL:         target,
		ContentType: "text/html",
		Body: []byte(`<html><body>
<a href="/next">go</a>
<a rel="nofollow" href="/blocked">blocked</a>
words here
</body></html>`),
	}
}

func runOneJob(
	t *testing.T,
	p *pipeline.Pipeline,
	frontier *recordingFrontier,
) doneCall {
	t.Helper()

	return runJob(t, p, frontier, crawljob.CrawlJob{
		URL: "https://example.com/", ProfileHandle: "h", Index: true,
	})
}

func runJob(
	t *testing.T,
	p *pipeline.Pipeline,
	frontier *recordingFrontier,
	job crawljob.CrawlJob,
) doneCall {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.RunWorkers(ctx, ctx, 1)
	frontier.jobs <- job
	select {
	case done := <-frontier.done:
		return done
	case <-time.After(2 * time.Second):
		t.Fatal("job never reached Done")

		return doneCall{}
	}
}

func TestPipelineDeliversIngestBatch(t *testing.T) {
	frontier := newRecordingFrontier()
	emitted := make(chan yagocrawlcontract.DocumentIngest, 1)
	sourceModifiedAt := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(
				_ context.Context,
				document yagocrawlcontract.DocumentIngest,
				_ []yagomodel.RWIPosting,
				_ yagomodel.URIMetadataRow,
				e ingest.Envelope,
			) error {
				if e.SourceURL != "https://example.com/" {
					t.Errorf("envelope source = %q", e.SourceURL)
				}
				if !e.SourceModifiedAt.Equal(sourceModifiedAt) {
					t.Errorf("envelope source modification = %v", e.SourceModifiedAt)
				}
				emitted <- document
				return nil
			},
		),
	)
	runJob(t, p, frontier, crawljob.CrawlJob{
		URL:              "https://example.com/",
		ProfileHandle:    "h",
		Index:            true,
		SourceModifiedAt: sourceModifiedAt,
	})
	select {
	case document := <-emitted:
		if document.NormalizedURL != "https://example.com/" {
			t.Errorf("document URL = %q", document.NormalizedURL)
		}
		if document.ContentType != "text/html" {
			t.Errorf("document content type = %q", document.ContentType)
		}
		if document.ExtractedText == "" {
			t.Fatal("document extracted text is empty")
		}
	case <-time.After(time.Second):
		t.Fatal("no batch emitted")
	}
	if len(frontier.submitted) != 1 || len(frontier.submitted[0].Followable) != 1 {
		t.Errorf("expected one submitted link set, got %v", frontier.submitted)
	}
}

func TestPipelineSubmitsNoFollowLinksSeparately(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
				return htmlPageWithNoFollow(), nil
			},
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
	if len(frontier.submitted) != 1 {
		t.Fatalf("submitted = %v", frontier.submitted)
	}
	got := frontier.submitted[0]
	if len(got.Followable) != 1 || got.Followable[0] != "/next" {
		t.Fatalf("followable links = %v", got.Followable)
	}
	if len(got.NoFollow) != 1 || got.NoFollow[0] != "/blocked" {
		t.Fatalf("nofollow links = %v", got.NoFollow)
	}
}

func TestPipelineFollowsButDoesNotIndexNonIndexablePage(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return htmlPage(), nil
		}),
		indexFunc(func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error) {
			t.Error("index should not run for a non-indexable page")
			return pageindex.Artifacts{}, nil
		}),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run for a non-indexable page")
				return nil
			},
		),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.RunWorkers(ctx, ctx, 1)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/", ProfileHandle: "h", Index: false}
	select {
	case done := <-frontier.done:
		if done.reason != "crawl profile disabled indexing" {
			t.Fatalf("non-indexing reason = %q", done.reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("job never reached Done")
	}
	if len(frontier.submitted) != 1 {
		t.Fatalf("non-indexable page should still follow links, submitted = %v", frontier.submitted)
	}
}

func TestPipelineDropsRejectedPages(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("bot wall: %w", pagefetch.ErrPageRejected)
		}),
		indexFunc(func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error) {
			t.Error("index should not run for rejected page")
			return pageindex.Artifacts{}, nil
		}),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run for rejected page")
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
	if len(frontier.submitted) != 0 {
		t.Errorf("rejected page should submit no links, got %v", frontier.submitted)
	}
}

func TestPipelineFinishesJobOnFetchError(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, errors.New("boom")
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
}

func TestPipelineRecordsStableInvalidURLAndUnsupportedContentReasons(t *testing.T) {
	invalidFrontier := newRecordingFrontier()
	invalidPipeline := pipeline.NewPipeline(
		invalidFrontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Fatal("invalid URL reached fetch")

			return pagefetch.FetchedPage{}, nil
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)
	invalid := runJob(t, invalidPipeline, invalidFrontier, crawljob.CrawlJob{URL: "://bad"})
	if invalid.outcome.Failed != 1 || invalid.reason != "crawl URL could not be parsed" {
		t.Fatalf("invalid URL outcome = %+v %q", invalid.outcome, invalid.reason)
	}

	unsupportedFrontier := newRecordingFrontier()
	unsupportedPipeline := pipeline.NewPipeline(
		unsupportedFrontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			target, err := url.Parse("https://example.com/archive.zip")
			if err != nil {
				t.Fatal(err)
			}

			return pagefetch.FetchedPage{
				URL: target, ContentType: "application/zip", Body: []byte("archive"),
			}, nil
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)
	unsupported := runJob(t, unsupportedPipeline, unsupportedFrontier, crawljob.CrawlJob{
		URL: "https://example.com/archive.zip", Index: true,
	})
	if unsupported.outcome.Failed != 1 ||
		unsupported.reason != "content type is not enabled for this crawl" {
		t.Fatalf("unsupported outcome = %+v %q", unsupported.outcome, unsupported.reason)
	}

	rejectedFrontier := newRecordingFrontier()
	rejectedPipeline := pipeline.NewPipeline(
		rejectedFrontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf(
				"provider detail: %w",
				pagefetch.ErrUnsupportedContentType,
			)
		}),
		pageindex.NewIndexBuilder(),
		okEmitter(),
	)
	rejected := runOneJob(t, rejectedPipeline, rejectedFrontier)
	if rejected.outcome.Failed != 1 ||
		rejected.reason != "content type is not enabled for this crawl" ||
		strings.Contains(rejected.reason, "provider detail") {
		t.Fatalf("rejected content outcome = %+v %q", rejected.outcome, rejected.reason)
	}
}

func TestPipelineMarksJobFailedOnEmitError(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return errors.New("emit failed")
			},
		),
	)
	if done := runOneJob(t, p, frontier); done.outcome.Failed != 1 ||
		done.reason != "document ingest delivery failed" {
		t.Error("emit failure should mark the job as delivery-failed so the order naks")
	}
}

func TestPipelineFinishesJobOnIndexError(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		indexFunc(func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error) {
			return pageindex.Artifacts{}, errors.New("index failed")
		}),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run after index error")
				return nil
			},
		),
	)
	if done := runOneJob(t, p, frontier); done.outcome.Failed != 1 ||
		done.reason != "document indexing failed" {
		t.Fatalf("index failure outcome = %+v %q", done.outcome, done.reason)
	}
}

func TestPipelineStopsWhenJobsClose(t *testing.T) {
	frontier := newRecordingFrontier()
	close(frontier.jobs)
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Error("fetch should not run after jobs close")
			return pagefetch.FetchedPage{}, nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run after jobs close")
				return nil
			},
		),
	)

	done := make(chan struct{})
	go func() {
		p.RunWorkers(context.Background(), context.Background(), 1)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not stop after jobs close")
	}
}

func TestPipelineStopsWhenContextIsCanceled(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Error("fetch should not run after context cancellation")
			return pagefetch.FetchedPage{}, nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run after context cancellation")
				return nil
			},
		),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		p.RunWorkers(ctx, ctx, 1)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not stop after context cancellation")
	}
}

func TestPipelineAbandonsFetchCancelledByShutdown(t *testing.T) {
	frontier := newRecordingFrontier()
	started := make(chan struct{})
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(ctx context.Context, _ *url.URL) (pagefetch.FetchedPage, error) {
			close(started)
			<-ctx.Done()

			return pagefetch.FetchedPage{}, ctx.Err()
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(func(
			context.Context,
			yagocrawlcontract.DocumentIngest,
			[]yagomodel.RWIPosting,
			yagomodel.URIMetadataRow,
			ingest.Envelope,
		) error {
			t.Fatal("cancelled fetch reached emit")

			return nil
		}),
	)
	acceptCtx, cancelAccept := context.WithCancel(context.Background())
	defer cancelAccept()
	fetchCtx, cancelFetch := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.RunWorkers(acceptCtx, fetchCtx, 1)
		close(done)
	}()
	job := crawljob.CrawlJob{URL: "https://example.com/unfinished"}
	frontier.jobs <- job
	<-started
	cancelFetch()
	select {
	case abandoned := <-frontier.abandoned:
		if abandoned.URL != job.URL {
			t.Fatalf("abandoned URL = %q, want %q", abandoned.URL, job.URL)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled fetch was not abandoned")
	}
	select {
	case completed := <-frontier.done:
		t.Fatalf("cancelled fetch completed: %+v", completed)
	default:
	}
	cancelAccept()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pipeline did not stop")
	}
}

func TestPipelineFinishesJobOnBadURL(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			t.Error("fetch should not run for bad job URL")
			return pagefetch.FetchedPage{}, nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run for bad job URL")
				return nil
			},
		),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.RunWorkers(ctx, ctx, 1)
	frontier.jobs <- crawljob.CrawlJob{URL: "://bad"}

	select {
	case <-frontier.done:
	case <-time.After(2 * time.Second):
		t.Fatal("bad URL job was not marked done")
	}
}

// TestPipelineGatesContentTypeByJobToggles pins the CRAWL-17 pipeline gate:
// a PDF response is dropped when the job's PDF toggle is off (counted as a
// failed fetch, nothing emitted) and flows through to the emitter when the
// toggle is on.
func TestPipelineGatesContentTypeByJobToggles(t *testing.T) {
	pdfBody := []byte("%PDF-1.4 not really parsed here")
	pdfFetch := fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
		target, _ := url.Parse("https://example.com/doc.pdf")

		return pagefetch.FetchedPage{
			URL: target, ContentType: "application/pdf", Body: pdfBody,
		}, nil
	})

	emittedCount := 0
	build := func() (*recordingFrontier, *pipeline.Pipeline) {
		frontier := newRecordingFrontier()

		return frontier, pipeline.NewPipeline(
			frontier,
			pdfFetch,
			pageindex.NewIndexBuilder(),
			emitFunc(func(
				context.Context,
				yagocrawlcontract.DocumentIngest,
				[]yagomodel.RWIPosting,
				yagomodel.URIMetadataRow,
				ingest.Envelope,
			) error {
				emittedCount++

				return nil
			}),
		)
	}

	frontier, gated := build()
	ctx, cancel := context.WithCancel(context.Background())
	go gated.RunWorkers(ctx, ctx, 1)
	frontier.jobs <- crawljob.CrawlJob{
		URL: "https://example.com/doc.pdf", ProfileHandle: "h", Index: true,
	}
	select {
	case <-frontier.done:
	case <-time.After(2 * time.Second):
		t.Fatal("gated job did not finish")
	}
	cancel()
	if emittedCount != 0 {
		t.Fatalf("pdf with the toggle off must not reach the emitter: %d", emittedCount)
	}

	frontier2, open := build()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go open.RunWorkers(ctx2, ctx2, 1)
	frontier2.jobs <- crawljob.CrawlJob{
		URL: "https://example.com/doc.pdf", ProfileHandle: "h", Index: true,
		Formats: yagocrawlcontract.FormatToggles{PDF: true},
	}
	select {
	case <-frontier2.done:
	case <-time.After(2 * time.Second):
		t.Fatal("open job did not finish")
	}
	if emittedCount != 0 {
		t.Fatalf("unparseable pdf stub must pass the gate yet emit nothing: %d", emittedCount)
	}
}
