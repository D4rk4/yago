package pipeline

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type retryExecutionFrontier struct {
	consumed bool
	consume  bool
	deferred bool
	done     bool
	schedule bool
	retryAt  time.Time
}

func (*retryExecutionFrontier) Take(context.Context) (crawljob.CrawlJob, bool) {
	return crawljob.CrawlJob{}, false
}

func (*retryExecutionFrontier) Submit(
	context.Context,
	crawljob.CrawlJob,
	crawljob.DiscoveredLinks,
) uint64 {
	return 0
}

func (f *retryExecutionFrontier) Done(crawljob.CrawlJob, yagocrawlcontract.CrawlRunTally) {
	f.done = true
}

func (*retryExecutionFrontier) Abandon(crawljob.CrawlJob) {}

func (*retryExecutionFrontier) ResolveRedirect(crawljob.CrawlJob, string) bool {
	return true
}

func (f *retryExecutionFrontier) Defer(
	_ context.Context,
	_ crawljob.CrawlJob,
	retryAt time.Time,
) (bool, bool) {
	f.consumed = true
	f.retryAt = retryAt
	f.deferred = f.schedule
	return f.consume, f.schedule
}

type pageRetryExecutionObserver struct {
	noopObserver
	scheduled int
}

func (o *pageRetryExecutionObserver) PageRetryScheduled() {
	o.scheduled++
}

func TestProcessDefersTransientFetchAndCompletesPermanentFetch(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		consumeRetry     bool
		consumeResult    bool
		scheduleRetry    bool
		wantDone         bool
		wantRetryCounter int
	}{
		{
			name: "transient network failure", err: &net.DNSError{IsTemporary: true},
			consumeRetry: true, consumeResult: true, scheduleRetry: true, wantRetryCounter: 1,
		},
		{
			name: "retry persistence failure", err: &net.DNSError{IsTemporary: true},
			consumeRetry: true, consumeResult: true,
		},
		{
			name: "frontier declines retry", err: &net.DNSError{IsTemporary: true},
			consumeRetry: true, wantDone: true,
		},
		{
			name: "permanent status", err: &pagefetch.HTTPStatusError{Status: 404}, wantDone: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontier := &retryExecutionFrontier{
				consume: test.consumeResult, schedule: test.scheduleRetry,
			}
			observer := &pageRetryExecutionObserver{}
			crawler := NewPipeline(
				frontier,
				pageSourceFunc(func(context.Context, pagefetch.FetchRequest) (pagefetch.FetchedPage, error) {
					return pagefetch.FetchedPage{}, test.err
				}),
				nil,
				nil,
				WithObserver(observer),
			)
			if err := crawler.process(t.Context(), crawljob.CrawlJob{
				URL: "https://example.test/page",
			}); err == nil {
				t.Fatal("fetch error was not returned")
			}
			if frontier.consumed != test.consumeRetry || frontier.deferred != test.scheduleRetry ||
				frontier.done != test.wantDone {
				t.Fatalf(
					"consumed/deferred/done = %v/%v/%v",
					frontier.consumed,
					frontier.deferred,
					frontier.done,
				)
			}
			if test.scheduleRetry && frontier.retryAt.IsZero() {
				t.Fatal("transient fetch failure has no retry deadline")
			}
			if observer.scheduled != test.wantRetryCounter {
				t.Fatalf(
					"scheduled retry observations = %d, want %d",
					observer.scheduled,
					test.wantRetryCounter,
				)
			}
		})
	}
}

type pageSourceFunc func(context.Context, pagefetch.FetchRequest) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(
	ctx context.Context,
	request pagefetch.FetchRequest,
) (pagefetch.FetchedPage, error) {
	return f(ctx, request)
}

func TestProcessDoesNotRetryCanceledFetch(t *testing.T) {
	frontier := &retryExecutionFrontier{}
	crawler := NewPipeline(
		frontier,
		pageSourceFunc(func(ctx context.Context, _ pagefetch.FetchRequest) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, ctx.Err()
		}),
		nil,
		nil,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := crawler.process(
		ctx,
		crawljob.CrawlJob{URL: "https://example.test/page"},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("process error = %v", err)
	}
	if frontier.deferred || frontier.done {
		t.Fatalf("cancelled work deferred or completed: %v/%v", frontier.deferred, frontier.done)
	}
}
