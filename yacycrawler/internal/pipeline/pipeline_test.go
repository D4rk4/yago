package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/crawljob"
	"github.com/D4rk4/yago/yacycrawler/internal/ingest"
	"github.com/D4rk4/yago/yacycrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yacycrawler/internal/pageindex"
	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
	"github.com/D4rk4/yago/yacycrawler/internal/pipeline"
	"github.com/D4rk4/yago/yacymodel"
)

type recordingFrontier struct {
	jobs      chan crawljob.CrawlJob
	submitted []crawljob.DiscoveredLinks
	done      chan crawljob.CrawlJob
}

func newRecordingFrontier() *recordingFrontier {
	return &recordingFrontier{
		jobs: make(chan crawljob.CrawlJob, 1),
		done: make(chan crawljob.CrawlJob, 8),
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

func (f *recordingFrontier) Done(work crawljob.CrawlJob) { f.done <- work }

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
	yacycrawlcontract.DocumentIngest,
	[]yacymodel.RWIPosting,
	yacymodel.URIMetadataRow,
	ingest.Envelope,
) error

func (f emitFunc) Emit(
	ctx context.Context,
	document yacycrawlcontract.DocumentIngest,
	postings []yacymodel.RWIPosting,
	metadata yacymodel.URIMetadataRow,
	envelope ingest.Envelope,
) error {
	return f(ctx, document, postings, metadata, envelope)
}

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
) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.RunWorkers(ctx, ctx, 1)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/", ProfileHandle: "h"}
	select {
	case <-frontier.done:
	case <-time.After(2 * time.Second):
		t.Fatal("job never reached Done")
	}
}

func TestPipelineDeliversIngestBatch(t *testing.T) {
	frontier := newRecordingFrontier()
	emitted := make(chan yacycrawlcontract.DocumentIngest, 1)
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(
				_ context.Context,
				document yacycrawlcontract.DocumentIngest,
				_ []yacymodel.RWIPosting,
				_ yacymodel.URIMetadataRow,
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
}

func TestPipelineFinishesJobOnEmitError(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, *url.URL) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
				return errors.New("emit failed")
			},
		),
	)
	runOneJob(t, p, frontier)
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, yacycrawlcontract.DocumentIngest, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
