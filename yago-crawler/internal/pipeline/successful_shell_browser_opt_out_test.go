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

func TestPipelineDisablesSuccessfulShellRenderingPerJob(t *testing.T) {
	frontier := newRecordingFrontier()
	browserCalls := 0
	source := pagefetch.NewBrowserFallbackPageSource(
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{
				URL:         target,
				ContentType: "text/html",
				Body:        []byte("<html><body><script src=\"/app.js\"></script></body></html>"),
			}, nil
		}),
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			browserCalls++

			return pagefetch.FetchedPage{
				URL:         target,
				ContentType: "text/html",
				Body: []byte(
					"<html><body>Rendered content has enough indexable words.</body></html>",
				),
			}, nil
		}),
		func(pagefetch.FetchedPage) bool { return true },
	)
	crawlerPipeline := pipeline.NewPipeline(
		frontier,
		source,
		pageindex.NewIndexBuilder(),
		emitFunc(func(
			context.Context,
			yagocrawlcontract.DocumentIngest,
			[]yagomodel.RWIPosting,
			yagomodel.URIMetadataRow,
			ingest.Envelope,
		) error {
			return nil
		}),
	)
	frontier.jobs = make(chan crawljob.CrawlJob, 2)
	frontier.jobs <- crawljob.CrawlJob{
		URL: "https://example.test/disabled", ProfileHandle: "disabled", DisableBrowser: true,
	}
	frontier.jobs <- crawljob.CrawlJob{
		URL: "https://example.test/enabled", ProfileHandle: "enabled",
	}
	close(frontier.jobs)
	crawlerPipeline.RunWorkers(context.Background(), context.Background(), 1)
	if browserCalls != 1 {
		t.Fatalf("browser calls = %d, want 1", browserCalls)
	}
}
