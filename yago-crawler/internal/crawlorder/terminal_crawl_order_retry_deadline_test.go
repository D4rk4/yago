package crawlorder

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTerminalCrawlOrderRequeueUsesDurableRetryDeadlineWhenAvailable(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		got := observeTerminalCrawlOrderRequeue(t, 7*time.Minute)
		if !got.settled || got.nakCalls != 1 ||
			got.retryAfter != 7*time.Minute || got.terminalCalls != 0 {
			t.Fatalf("deadline settlement = %+v", got)
		}
	})
	t.Run("default settlement", func(t *testing.T) {
		got := observeTerminalCrawlOrderRequeue(t, 0)
		if !got.settled || got.nakCalls != 0 || got.terminalCalls != 1 {
			t.Fatalf("default settlement = %+v", got)
		}
	})
}

type terminalCrawlOrderRequeueObservation struct {
	settled       bool
	nakCalls      int
	retryAfter    time.Duration
	terminalCalls int
}

func observeTerminalCrawlOrderRequeue(
	t *testing.T,
	retryAfter time.Duration,
) terminalCrawlOrderRequeueObservation {
	t.Helper()
	observation := terminalCrawlOrderRequeueObservation{}
	delivery := CrawlOrderDelivery{
		Nak: func(context.Context) error {
			t.Fatalf("ordinary NAK used")
			return nil
		},
		NakAfter: func(_ context.Context, received time.Duration) error {
			observation.nakCalls++
			observation.retryAfter = received
			return nil
		},
		settleTerminal: func(context.Context, terminalRunSettlement) error {
			observation.terminalCalls++
			return nil
		},
	}
	observation.settled = settleTerminalCrawlOrder(context.Background(),
		terminalCrawlOrderDisposition{
			delivery: delivery, disposition: crawlOrderRequeued, retryAfter: retryAfter,
			state: yagocrawlcontract.CrawlRunFinished,
		})
	return observation
}
