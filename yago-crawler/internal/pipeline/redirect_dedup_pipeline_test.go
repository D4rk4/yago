package pipeline_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/ingest"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func redirectingPipeline(
	frontier *recordingFrontier,
	finalURL string,
	emitted *int,
) *pipeline.Pipeline {
	return pipeline.NewPipeline(
		frontier,
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			target, _ := url.Parse(finalURL)

			return pagefetch.FetchedPage{
				URL:         target,
				ContentType: "text/html",
				Body:        []byte(`<html><body><a href="/next">go</a> words</body></html>`),
			}, nil
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				*emitted++

				return nil
			},
		),
	)
}

func TestPipelineSkipsRedirectToVisitedTarget(t *testing.T) {
	frontier := newRecordingFrontier()
	frontier.resolveRedirect = func(crawljob.CrawlJob, string) bool { return false }
	emitted := 0
	p := redirectingPipeline(frontier, "https://example.com/final", &emitted)

	done := runJob(t, p, frontier, crawljob.CrawlJob{
		URL: "https://example.com/start", ProfileHandle: "h", Index: true,
	})
	if done.outcome.Failed != 0 {
		t.Error("a duplicate redirect target must not mark the job delivery-failed")
	}
	if done.reason != "redirect target was not admitted" {
		t.Fatalf("redirect outcome reason = %q", done.reason)
	}
	if emitted != 0 {
		t.Errorf("emitted = %d, want 0", emitted)
	}
	if len(frontier.submitted) != 0 {
		t.Errorf("submitted = %v, want none", frontier.submitted)
	}
	if len(frontier.redirects) != 1 || frontier.redirects[0] != "https://example.com/final" {
		t.Errorf("redirect checks = %v", frontier.redirects)
	}
}

func TestPipelineIndexesAdmittedRedirectTarget(t *testing.T) {
	frontier := newRecordingFrontier()
	emitted := 0
	p := redirectingPipeline(frontier, "https://example.com/final", &emitted)

	runJob(t, p, frontier, crawljob.CrawlJob{
		URL: "https://example.com/start", ProfileHandle: "h", Index: true,
	})
	if emitted != 1 {
		t.Errorf("emitted = %d, want 1", emitted)
	}
	if len(frontier.submitted) != 1 {
		t.Errorf("submitted = %d sets, want 1", len(frontier.submitted))
	}
}

func TestPipelineResolvesDirectResponseForCheckpointCleanup(t *testing.T) {
	frontier := newRecordingFrontier()
	emitted := 0
	p := redirectingPipeline(frontier, "https://example.com/", &emitted)

	runOneJob(t, p, frontier)
	if len(frontier.redirects) != 1 || frontier.redirects[0] != "https://example.com/" {
		t.Errorf("redirect checks = %v, want direct response", frontier.redirects)
	}
	if emitted != 1 {
		t.Errorf("emitted = %d, want 1", emitted)
	}
}

func TestPipelineIndexesRedirectTargetOncePerRun(t *testing.T) {
	frontier := newRecordingFrontier()
	visited := make(map[string]struct{})
	frontier.resolveRedirect = func(_ crawljob.CrawlJob, finalURL string) bool {
		if _, seen := visited[finalURL]; seen {
			return false
		}
		visited[finalURL] = struct{}{}

		return true
	}
	emitted := 0
	p := redirectingPipeline(frontier, "https://example.com/final", &emitted)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.RunWorkers(ctx, ctx, 1)
	for _, source := range []string{"https://example.com/a", "https://example.com/b"} {
		frontier.jobs <- crawljob.CrawlJob{URL: source, ProfileHandle: "h", Index: true}
		select {
		case <-frontier.done:
		case <-time.After(2 * time.Second):
			t.Fatal("job never reached Done")
		}
	}
	if emitted != 1 {
		t.Errorf("emitted = %d, want 1 (second redirect must dedup)", emitted)
	}
	if len(frontier.submitted) != 1 {
		t.Errorf("submitted = %d sets, want 1", len(frontier.submitted))
	}
}
