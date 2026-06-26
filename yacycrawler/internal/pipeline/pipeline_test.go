package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/ingest"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pipeline"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type recordingFrontier struct {
	jobs      chan crawljob.CrawlJob
	submitted [][]string
	done      chan crawljob.CrawlJob
}

func newRecordingFrontier() *recordingFrontier {
	return &recordingFrontier{
		jobs: make(chan crawljob.CrawlJob, 1),
		done: make(chan crawljob.CrawlJob, 8),
	}
}

func (f *recordingFrontier) Jobs() <-chan crawljob.CrawlJob { return f.jobs }

func (f *recordingFrontier) Submit(_ context.Context, _ crawljob.CrawlJob, links []string) {
	f.submitted = append(f.submitted, links)
}

func (f *recordingFrontier) Done(work crawljob.CrawlJob) { f.done <- work }

type fetchFunc func(context.Context, string) (pagefetch.FetchedPage, error)

func (f fetchFunc) Fetch(ctx context.Context, rawURL string) (pagefetch.FetchedPage, error) {
	return f(ctx, rawURL)
}

type indexFunc func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error)

func (f indexFunc) Build(
	p pageparse.ParsedPage,
	s pageparse.PageStats,
) (pageindex.Artifacts, error) {
	return f(p, s)
}

type emitFunc func(context.Context, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error

func (f emitFunc) Emit(
	ctx context.Context,
	postings []yacymodel.RWIPosting,
	metadata yacymodel.URIMetadataRow,
	envelope ingest.Envelope,
) error {
	return f(ctx, postings, metadata, envelope)
}

func htmlPage() pagefetch.FetchedPage {
	return pagefetch.FetchedPage{
		URL:         "https://example.com/",
		ContentType: "text/html",
		Body:        []byte(`<html><body><a href="/next">go</a> words here</body></html>`),
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
	go p.RunWorkers(ctx, 1)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://example.com/", ProfileHandle: "h"}
	select {
	case <-frontier.done:
	case <-time.After(2 * time.Second):
		t.Fatal("job never reached Done")
	}
}

func TestPipelineDeliversIngestBatch(t *testing.T) {
	frontier := newRecordingFrontier()
	emitted := make(chan ingest.Envelope, 1)
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, string) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(_ context.Context, _ []yacymodel.RWIPosting, _ yacymodel.URIMetadataRow, e ingest.Envelope) error {
				emitted <- e
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
	select {
	case e := <-emitted:
		if e.SourceURL != "https://example.com/" {
			t.Errorf("envelope source = %q", e.SourceURL)
		}
	case <-time.After(time.Second):
		t.Fatal("no batch emitted")
	}
	if len(frontier.submitted) != 1 || len(frontier.submitted[0]) != 1 {
		t.Errorf("expected one submitted link set, got %v", frontier.submitted)
	}
}

func TestPipelineDropsRejectedPages(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, string) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, fmt.Errorf("bot wall: %w", pagefetch.ErrPageRejected)
		}),
		indexFunc(func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error) {
			t.Error("index should not run for rejected page")
			return pageindex.Artifacts{}, nil
		}),
		emitFunc(
			func(context.Context, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
		fetchFunc(func(context.Context, string) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, errors.New("boom")
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, string) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
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
			func(context.Context, string) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		indexFunc(func(pageparse.ParsedPage, pageparse.PageStats) (pageindex.Artifacts, error) {
			return pageindex.Artifacts{}, errors.New("index failed")
		}),
		emitFunc(
			func(context.Context, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run after index error")
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
}
