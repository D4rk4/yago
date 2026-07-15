package pipeline_test

import (
	"context"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/ingest"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pipeline"
	"github.com/D4rk4/yago/yagocrawler/internal/robots"
	"github.com/D4rk4/yago/yagomodel"
)

type recordingFeedback struct {
	throttled  []time.Duration
	successes  int
	lastTarget string
}

func (f *recordingFeedback) Throttled(rawURL string, retryAfter time.Duration, _ time.Time) {
	f.throttled = append(f.throttled, retryAfter)
	f.lastTarget = rawURL
}

func (f *recordingFeedback) Succeeded(string, time.Time) { f.successes++ }

type hostOutcomeFrontier struct {
	*recordingFrontier
	outcomes []bool
}

func (f *hostOutcomeFrontier) RecordHostFetchOutcome(
	_ context.Context,
	_ crawljob.CrawlJob,
	failed bool,
) {
	f.outcomes = append(f.outcomes, failed)
}

func TestPipelineFeedsHostLoadSignals(t *testing.T) {
	frontier := &hostOutcomeFrontier{recordingFrontier: newRecordingFrontier()}
	feedback := &recordingFeedback{}
	p := pipeline.NewPipeline(
		frontier,
		fetchFunc(func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
			switch target.Path {
			case "/throttled":
				return pagefetch.FetchedPage{}, &pagefetch.ThrottledError{
					Status:     429,
					RetryAfter: time.Minute,
				}
			case "/broken":
				return pagefetch.FetchedPage{}, pagefetch.ErrPageRejected
			case "/robots":
				return pagefetch.FetchedPage{}, robots.ErrDisallowed
			case "/connection":
				return pagefetch.FetchedPage{}, &net.DNSError{
					Err: "timeout", Name: target.Host, IsTimeout: true,
				}
			case "/unsupported":
				return pagefetch.FetchedPage{
					URL: target, ContentType: "application/x-proprietary", Body: []byte("body"),
				}, nil
			default:
				return htmlPage(), nil
			}
		}),
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
		pipeline.WithHostLoadFeedback(feedback),
	)

	frontier.jobs = make(chan crawljob.CrawlJob, 6)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://busy.example/throttled"}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://busy.example/broken"}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://busy.example/robots"}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://busy.example/connection"}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://busy.example/unsupported"}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://calm.example/page"}
	close(frontier.jobs)
	p.RunWorkers(context.Background(), context.Background(), 1)

	if len(feedback.throttled) != 2 ||
		feedback.throttled[0] != time.Minute || feedback.throttled[1] != 0 {
		t.Fatalf("throttle signals = %v", feedback.throttled)
	}
	if feedback.lastTarget != "https://busy.example/connection" {
		t.Fatalf("throttle target = %q", feedback.lastTarget)
	}
	if feedback.successes != 1 {
		t.Fatalf("successes = %d, want 1", feedback.successes)
	}
	if len(frontier.outcomes) != 3 || !frontier.outcomes[0] ||
		!frontier.outcomes[1] || frontier.outcomes[2] {
		t.Fatalf("host availability outcomes = %v", frontier.outcomes)
	}
}

// TestPipelineDisablesBrowserEscalationPerJob: a DisableBrowser job's fetch
// context carries the opt-out marker down to the fallback source.
func TestPipelineDisablesBrowserEscalationPerJob(t *testing.T) {
	frontier := newRecordingFrontier()
	fallbackCalls := 0
	source := pagefetch.NewFallbackPageSource(
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, pagefetch.ErrPageRejected
		}),
		fetchFunc(func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			fallbackCalls++
			return htmlPage(), nil
		}),
	)
	p := pipeline.NewPipeline(
		frontier,
		source,
		pageindex.NewIndexBuilder(),
		emitFunc(
			func(context.Context, yagocrawlcontract.DocumentIngest, []yagomodel.RWIPosting, yagomodel.URIMetadataRow, ingest.Envelope) error {
				return nil
			},
		),
	)

	frontier.jobs = make(chan crawljob.CrawlJob, 2)
	frontier.jobs <- crawljob.CrawlJob{URL: "https://a.example/plain", DisableBrowser: true}
	frontier.jobs <- crawljob.CrawlJob{URL: "https://a.example/rendered"}
	close(frontier.jobs)
	p.RunWorkers(context.Background(), context.Background(), 1)

	if fallbackCalls != 1 {
		t.Fatalf("browser fallback ran %d times, want 1 (only the default job)", fallbackCalls)
	}
}
