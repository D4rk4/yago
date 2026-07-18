package pipeline_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func insecureSelectionPipeline(
	t *testing.T,
	frontier *recordingFrontier,
	secureHits *int,
	insecureHits *int,
) *pipeline.Pipeline {
	t.Helper()

	return pipeline.NewPipeline(
		frontier,
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			*secureHits++

			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
		pipeline.WithInsecureFetcher(
			fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
				*insecureHits++

				return htmlPage(), nil
			}),
		),
	)
}

func TestPipelineRoutesTLSOptOutJobsThroughInsecureFetcher(t *testing.T) {
	frontier := newRecordingFrontier()
	var secure, insecure int
	p := insecureSelectionPipeline(t, frontier, &secure, &insecure)

	frontier.jobs = make(chan crawljob.CrawlJob, 2)
	frontier.jobs <- crawljob.CrawlJob{
		URL:                "https://example.com/",
		IgnoreTLSAuthority: true,
	}
	frontier.jobs <- crawljob.CrawlJob{
		URL: "https://example.com/strict",
	}
	close(frontier.jobs)
	p.RunWorkers(context.Background(), context.Background(), 1)

	if insecure != 1 || secure != 1 {
		t.Fatalf("insecure = %d secure = %d, want each chain used once", insecure, secure)
	}
}

func TestPipelineKeepsTLSOptOutJobsOnDefaultChainWithoutInsecureFetcher(t *testing.T) {
	frontier := newRecordingFrontier()
	fetched := 0
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			fetched++

			return htmlPage(), nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
		pipeline.WithInsecureFetcher(nil),
	)

	frontier.jobs <- crawljob.CrawlJob{
		URL:                "https://example.com/",
		IgnoreTLSAuthority: true,
	}
	close(frontier.jobs)
	p.RunWorkers(context.Background(), context.Background(), 1)

	if fetched != 1 {
		t.Fatalf("fetched = %d, want the default chain to serve the job", fetched)
	}
}
