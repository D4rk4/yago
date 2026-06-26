package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/ingest"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pipeline"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type recordingFrontier struct {
	jobs      chan crawlwork.CrawlJob
	submitted [][]string
	done      chan crawlwork.CrawlJob
}

func newRecordingFrontier() *recordingFrontier {
	return &recordingFrontier{
		jobs: make(chan crawlwork.CrawlJob, 1),
		done: make(chan crawlwork.CrawlJob, 8),
	}
}

func (f *recordingFrontier) Jobs() <-chan crawlwork.CrawlJob { return f.jobs }

func (f *recordingFrontier) Submit(_ context.Context, _ crawlwork.CrawlJob, links []string) {
	f.submitted = append(f.submitted, links)
}

func (f *recordingFrontier) Done(work crawlwork.CrawlJob) { f.done <- work }

type fetchFunc func(context.Context, string) (pagefetch.FetchedPage, error)

func (f fetchFunc) Fetch(ctx context.Context, rawURL string) (pagefetch.FetchedPage, error) {
	return f(ctx, rawURL)
}

type botWallFunc func(pagefetch.FetchedPage) bool

func (f botWallFunc) IsBotWall(page pagefetch.FetchedPage) bool { return f(page) }

type indexFunc func(crawlwork.ParsedPage, crawlwork.PageStats) (pageindex.Artifacts, error)

func (f indexFunc) Build(
	p crawlwork.ParsedPage,
	s crawlwork.PageStats,
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
	frontier.jobs <- crawlwork.CrawlJob{URL: "https://example.com/", ProfileHandle: "h"}
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
		botWallFunc(func(pagefetch.FetchedPage) bool { return false }),
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

func TestPipelineDropsBotWallPages(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(
			func(context.Context, string) (pagefetch.FetchedPage, error) { return htmlPage(), nil },
		),
		botWallFunc(func(pagefetch.FetchedPage) bool { return true }),
		indexFunc(func(crawlwork.ParsedPage, crawlwork.PageStats) (pageindex.Artifacts, error) {
			t.Error("index should not run for bot wall page")
			return pageindex.Artifacts{}, nil
		}),
		emitFunc(
			func(context.Context, []yacymodel.RWIPosting, yacymodel.URIMetadataRow, ingest.Envelope) error {
				t.Error("emit should not run for bot wall page")
				return nil
			},
		),
	)
	runOneJob(t, p, frontier)
	if len(frontier.submitted) != 0 {
		t.Errorf("bot wall page should submit no links, got %v", frontier.submitted)
	}
}

func TestPipelineFinishesJobOnFetchError(t *testing.T) {
	frontier := newRecordingFrontier()
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, string) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, errors.New("boom")
		}),
		botWallFunc(func(pagefetch.FetchedPage) bool { return false }),
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
		botWallFunc(func(pagefetch.FetchedPage) bool { return false }),
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
		botWallFunc(func(pagefetch.FetchedPage) bool { return false }),
		indexFunc(func(crawlwork.ParsedPage, crawlwork.PageStats) (pageindex.Artifacts, error) {
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
