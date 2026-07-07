package crawlorder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

type scriptedExpander struct {
	requests []yagocrawlcontract.CrawlRequest
	err      error
}

func (e scriptedExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	return e.requests, e.err
}

func consumerProfile() yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
}

func waitCallback(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery callback was not called")
	}
}

func TestAcceptLogsTermError(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	)
	termed := make(chan struct{})

	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{
			Profile: yagocrawlcontract.CrawlProfile{URLMustMatch: "("},
		},
		Term: func(context.Context) error {
			close(termed)
			return errors.New("term failed")
		},
	})

	waitCallback(t, termed)
}

func TestAcceptLogsAckError(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	)
	acked := make(chan struct{})

	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{Profile: consumerProfile()},
		Ack: func(context.Context) error {
			close(acked)
			return errors.New("ack failed")
		},
	})

	waitCallback(t, acked)
}

func TestAcceptTermsExpansionError(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
		scriptedExpander{err: errors.New("expand failed")},
	)
	termed := make(chan struct{})

	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{Profile: consumerProfile()},
		Term: func(context.Context) error {
			close(termed)
			return errors.New("term failed")
		},
	})

	waitCallback(t, termed)
}

func TestAcceptSeedsExpandedRequests(t *testing.T) {
	f := frontier.NewFrontier(1, nil)
	profile := consumerProfile()
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		f,
		scriptedExpander{requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.org/from-sitemap",
			ProfileHandle: profile.Handle,
		}}},
	)
	acked := make(chan struct{})

	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{
			Profile: profile,
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL:           "https://example.org/sitemap.xml",
				Mode:          yagocrawlcontract.CrawlRequestModeSitemap,
				ProfileHandle: profile.Handle,
			}},
		},
		Ack: func(context.Context) error {
			close(acked)
			return nil
		},
	})
	job := <-f.Jobs()
	if job.URL != "https://example.org/from-sitemap" {
		t.Fatalf("job URL = %q", job.URL)
	}
	f.Done(job, false)
	waitCallback(t, acked)
}

func TestAcceptNaksCanceledRunAndLogsNakError(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	naked := make(chan struct{})

	consumer.accept(ctx, CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{Profile: consumerProfile()},
		Nak: func(context.Context) error {
			close(naked)
			return errors.New("nak failed")
		},
	})

	waitCallback(t, naked)
}

func TestAcceptNaksFrontierCancelledRun(t *testing.T) {
	f := frontier.NewFrontier(1, nil)
	provenance := []byte("cancel-me")
	f.Cancel(provenance)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		f,
	)
	naked := make(chan struct{})

	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{Profile: consumerProfile(), Provenance: provenance},
		Nak: func(context.Context) error {
			close(naked)

			return nil
		},
	})

	waitCallback(t, naked)
	if f.WasCancelled(provenance) {
		t.Fatal("finishRun should clear the cancelled mark once the run settles")
	}
}

func TestPassThroughRequestExpanderRejectsUnknownMode(t *testing.T) {
	_, err := passThroughRequestExpander{}.Expand(
		context.Background(),
		[]yagocrawlcontract.CrawlRequest{{Mode: "archive"}},
	)
	if err == nil {
		t.Fatal("unknown mode should fail")
	}
}
