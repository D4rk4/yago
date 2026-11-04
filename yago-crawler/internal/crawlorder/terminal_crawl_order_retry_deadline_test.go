package crawlorder

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTerminalCrawlOrderRequeueUsesDurableRetryDeadlineWhenAvailable(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryAfter time.Duration
		wantNak    bool
	}{
		{name: "deadline", retryAfter: 7 * time.Minute, wantNak: true},
		{name: "default settlement", wantNak: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var received time.Duration
			nakCalls := 0
			terminalCalls := 0
			delivery := CrawlOrderDelivery{
				Nak: func(context.Context) error {
					t.Fatalf("ordinary NAK used")
					return nil
				},
				NakAfter: func(_ context.Context, retryAfter time.Duration) error {
					nakCalls++
					received = retryAfter
					return nil
				},
				settleTerminal: func(context.Context, terminalRunSettlement) error {
					terminalCalls++
					return nil
				},
			}
			settled := settleTerminalCrawlOrder(context.Background(), terminalCrawlOrderDisposition{
				delivery: delivery, disposition: crawlOrderRequeued, retryAfter: test.retryAfter,
				state: yagocrawlcontract.CrawlRunFinished,
			})
			if !settled {
				t.Fatal("crawl order did not settle")
			}
			if test.wantNak {
				if nakCalls != 1 || received != test.retryAfter || terminalCalls != 0 {
					t.Fatalf("deadline settlement: nak=%d retryAfter=%s terminal=%d", nakCalls, received, terminalCalls)
				}
			} else if nakCalls != 0 || terminalCalls != 1 {
				t.Fatalf("default settlement: nak=%d terminal=%d", nakCalls, terminalCalls)
			}
		})
	}
}
