package crawlorder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yacycrawler/internal/frontier"
)

func consumerProfile() yacycrawlcontract.CrawlProfile {
	return yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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
		Order: yacycrawlcontract.CrawlOrder{
			Profile: yacycrawlcontract.CrawlProfile{URLMustMatch: "("},
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
		Order: yacycrawlcontract.CrawlOrder{Profile: consumerProfile()},
		Ack: func(context.Context) error {
			close(acked)
			return errors.New("ack failed")
		},
	})

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
		Order: yacycrawlcontract.CrawlOrder{Profile: consumerProfile()},
		Nak: func(context.Context) error {
			close(naked)
			return errors.New("nak failed")
		},
	})

	waitCallback(t, naked)
}
