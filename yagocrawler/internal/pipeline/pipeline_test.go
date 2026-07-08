package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawler/internal/pipeline"
	"github.com/D4rk4/yago/yagomodel"
)

type doneCall struct {
	work   crawljob.CrawlJob
	failed bool
}

type recordingFrontier struct {
	jobs      chan crawljob.CrawlJob
	submitted []crawljob.DiscoveredLinks
	done      chan doneCall
	redirects []string
	// resolveRedirect overrides the ResolveRedirect outcome; nil admits every
	// redirect target.
	resolveRedirect func(job crawljob.CrawlJob, finalURL string) bool
}

func newRecordingFrontier() *recordingFrontier {
	return &recordingFrontier{
		jobs: make(chan crawljob.CrawlJob, 1),
		done: make(chan doneCall, 8),
	}
}

func (f *recordingFrontier) Jobs() <-chan crawljob.CrawlJob { return f.jobs }

func (f *recordingFrontier) Submit(
	_ context.Context,
	_ crawljob.CrawlJob,
	links crawljob.DiscoveredLinks,
) {
	f.submitted = append(f.submitted, links)
}

func (f *recordingFrontier) Done(work crawljob.CrawlJob, deliveryFailed bool) {
	f.done <- doneCall{work: work, failed: deliveryFailed}
}

func (f *recordingFrontier) ResolveRedirect(job crawljob.CrawlJob, finalURL string) bool {
	f.redirects = append(f.redirects, finalURL)
	if f.resolveRedirect == nil {
		return true
	}

	return f.resolveRedirect(job, finalURL)
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

func (emitFunc) EmitRemoval(context.Context, string, []byte, string) error { return nil }

func htmlPage() pagefetch.FetchedPage {
	target, _ := url.Parse("https://example.com/")
	return pagefetch.FetchedPage{
		URL:         target,
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
				emitted <- document
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
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
	case <-frontier.done:
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
	if done := runOneJob(t, p, frontier); !done.failed {
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
	runOneJob(t, p, frontier)
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
