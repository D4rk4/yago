package crawlorder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type scriptedExpander struct {
	requests []yagocrawlcontract.CrawlRequest
	err      error
}

type cancelingSuccessfulExpander struct {
	cancel context.CancelFunc
}

func (expander cancelingSuccessfulExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	expander.cancel()

	return nil, nil
}

func (e scriptedExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	return e.requests, e.err
}

type unexpectedExpander struct{}

func (unexpectedExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	panic("invalid order reached request expansion")
}

type scriptedPermanentExpansionError struct{}

func (scriptedPermanentExpansionError) Error() string {
	return "invalid expanded content"
}

func (scriptedPermanentExpansionError) Permanent() bool {
	return true
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
		Term: func(settlementCtx context.Context) error {
			if settlementCtx.Err() != nil {
				t.Errorf("term context error = %v, want live context", settlementCtx.Err())
			}
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

func TestAcceptClassifiesExpansionFailures(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		settlementErr error
		want          string
	}{
		{
			name:          "transient",
			err:           errors.New("expand failed"),
			settlementErr: errors.New("nak failed"),
			want:          "nak",
		},
		{name: "cancelled", err: context.Canceled, want: "nak"},
		{name: "deadline", err: context.DeadlineExceeded, want: "nak"},
		{name: "permanent", err: scriptedPermanentExpansionError{}, want: "term"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			consumer := NewCrawlOrderConsumer(
				boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
				frontier.NewFrontier(1, nil),
				scriptedExpander{err: test.err},
			)
			settled := make(chan string, 1)
			settle := func(result string) func(context.Context) error {
				return func(settlementCtx context.Context) error {
					if settlementCtx.Err() != nil {
						t.Errorf(
							"%s context error = %v, want live context",
							result,
							settlementCtx.Err(),
						)
					}
					settled <- result

					return test.settlementErr
				}
			}
			consumer.accept(context.Background(), CrawlOrderDelivery{
				LeaseID: "lease-" + test.name,
				Order: yagocrawlcontract.CrawlOrder{
					Profile: consumerProfile(),
					Requests: []yagocrawlcontract.CrawlRequest{{
						URL: "https://example.org/",
					}},
				},
				Ack:  settle("ack"),
				Nak:  settle("nak"),
				Term: settle("term"),
			})
			select {
			case got := <-settled:
				if got != test.want {
					t.Fatalf("settlement = %q, want %q", got, test.want)
				}
			case <-time.After(time.Second):
				t.Fatal("expansion failure was not settled")
			}
		})
	}
}

func TestAcceptRetainsSuccessfulExpansionInterruptedByShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
		cancelingSuccessfulExpander{cancel: cancel},
	)
	settlement := make(chan string, 3)
	consumer.accept(ctx, CrawlOrderDelivery{
		LeaseID: "cancelled-successful-expansion",
		Order: yagocrawlcontract.CrawlOrder{
			Profile: consumerProfile(),
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL: "https://example.org/",
			}},
		},
		Ack:  func(context.Context) error { settlement <- "ack"; return nil },
		Nak:  func(context.Context) error { settlement <- "nak"; return nil },
		Term: func(context.Context) error { settlement <- "term"; return nil },
	})
	select {
	case got := <-settlement:
		t.Fatalf("shutdown-interrupted successful expansion sent %s", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestManifestedRecoveryCompilesWithoutExpansion(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "permanent empty checkpoint",
			err:  scriptedPermanentExpansionError{},
		},
		{
			name: "transient checkpoint with pages",
			err:  errors.New("expansion temporarily unavailable"),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			consumer := NewCrawlOrderConsumer(
				boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
				frontier.NewFrontier(1, nil),
				scriptedExpander{err: test.err},
			)
			order := identityTestOrder()
			settled := make(chan string, 1)
			delivery := CrawlOrderDelivery{
				LeaseID: "recovered-" + test.name,
				Order:   order,
				Ack:     func(context.Context) error { settled <- "ack"; return nil },
				Nak:     func(context.Context) error { settled <- "nak"; return nil },
				Term:    func(context.Context) error { settled <- "term"; return nil },
			}
			if claim := consumer.active.claim(
				order.Provenance,
				delivery,
			); claim != activeOrderStartsRun {
				t.Fatalf("claim = %d, want start", claim)
			}
			_, _, prepared := consumer.prepareRecoveredSeedingOrder(
				t.Context(),
				order,
				delivery,
			)
			if !prepared {
				t.Fatal("manifested recovery was not prepared")
			}
			select {
			case got := <-settled:
				t.Fatalf("failed recovered expansion sent %s", got)
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestAcceptTermsInvalidCrawlRequestsBeforeExpansion(t *testing.T) {
	cases := []yagocrawlcontract.CrawlRequest{
		{URL: "https://example.org/", Mode: "archive"},
		{URL: "://invalid", Mode: yagocrawlcontract.CrawlRequestModeSitemap},
	}
	for _, request := range cases {
		consumer := NewCrawlOrderConsumer(
			boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
			frontier.NewFrontier(1, nil),
			unexpectedExpander{},
		)
		termed := make(chan struct{})
		consumer.accept(t.Context(), CrawlOrderDelivery{
			Order: yagocrawlcontract.CrawlOrder{
				Profile:  consumerProfile(),
				Requests: []yagocrawlcontract.CrawlRequest{request},
			},
			Nak: func(context.Context) error {
				t.Error("invalid order must not be requeued")

				return nil
			},
			Term: func(context.Context) error {
				close(termed)

				return nil
			},
		})
		waitCallback(t, termed)
	}
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
	job, ok := f.Take(t.Context())
	if !ok {
		t.Fatal("frontier closed before sitemap job")
	}
	if job.URL != "https://example.org/from-sitemap" {
		t.Fatalf("job URL = %q", job.URL)
	}
	f.Done(job, successfulPageOutcome())
	waitCallback(t, acked)
}

func TestAcceptPreservesAutomaticDiscoveryPriority(t *testing.T) {
	f := frontier.NewFrontier(8, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		f,
	)
	profile := consumerProfile()
	accept := func(provenance, rawURL string, priority yagocrawlcontract.CrawlOrderPriority) {
		consumer.accept(t.Context(), CrawlOrderDelivery{
			Order: yagocrawlcontract.CrawlOrder{
				Provenance: []byte(provenance),
				Priority:   priority,
				Profile:    profile,
				Requests: []yagocrawlcontract.CrawlRequest{{
					URL:           rawURL,
					ProfileHandle: profile.Handle,
				}},
			},
			Ack: func(context.Context) error { return nil },
			Nak: func(context.Context) error { return nil },
		})
	}
	accept("normal", "https://normal.example/", yagocrawlcontract.CrawlOrderPriorityNormal)
	accept(
		"automatic",
		"https://automatic.example/",
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	)

	for _, provenance := range []string{"automatic", "normal"} {
		job, ok := f.Take(t.Context())
		if !ok {
			t.Fatal("frontier closed before priority dispatch")
		}
		if got := string(job.Provenance); got != provenance {
			t.Fatalf("dispatch = %q, want %q", got, provenance)
		}
		f.Done(job, successfulPageOutcome())
	}
	consumer.WaitForSettlements()
}

func TestAcceptRetainsLeaseForRunStartedDuringShutdown(t *testing.T) {
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	settlement := make(chan string, 3)

	consumer.accept(ctx, CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{Profile: consumerProfile()},
		Ack:   func(context.Context) error { settlement <- "ack"; return nil },
		Nak:   func(context.Context) error { settlement <- "nak"; return nil },
		Term:  func(context.Context) error { settlement <- "term"; return nil },
	})
	consumer.WaitForSettlements()
	select {
	case got := <-settlement:
		t.Fatalf("shutdown-started run sent %s", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestAcceptTerminatesFrontierCancelledRun(t *testing.T) {
	f := frontier.NewFrontier(1, nil)
	provenance := []byte("cancel-me")
	f.Cancel(provenance)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		f,
	)
	terminated := make(chan struct{})

	consumer.accept(context.Background(), CrawlOrderDelivery{
		Order: yagocrawlcontract.CrawlOrder{Profile: consumerProfile(), Provenance: provenance},
		Term: func(context.Context) error {
			close(terminated)

			return nil
		},
	})

	waitCallback(t, terminated)
	if f.WasCancelled(provenance) {
		t.Fatal("finishRun should clear the cancelled mark once the run settles")
	}
}

func TestAcceptJoinsRedeliveredOrderToActiveRun(t *testing.T) {
	f := frontier.NewFrontier(4, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		f,
	)
	profile := consumerProfile()
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("reconnected-order"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.org/",
			ProfileHandle: profile.Handle,
		}},
	}
	acked := make(chan string, 2)
	delivery := func(leaseID string) CrawlOrderDelivery {
		return CrawlOrderDelivery{
			LeaseID: leaseID,
			Order:   order,
			Ack: func(context.Context) error {
				acked <- leaseID

				return nil
			},
		}
	}
	consumer.accept(t.Context(), delivery("stale-lease"))
	consumer.accept(t.Context(), delivery("current-lease"))
	job, ok := f.Take(t.Context())
	if !ok {
		t.Fatal("frontier closed before active run job")
	}
	f.Done(job, successfulPageOutcome())
	select {
	case leaseID := <-acked:
		if leaseID != "current-lease" {
			t.Fatalf("acknowledged lease %q, want current lease", leaseID)
		}
	case <-time.After(time.Second):
		t.Fatal("current lease was not acknowledged")
	}
	select {
	case leaseID := <-acked:
		t.Fatalf("unexpected additional acknowledgement for %q", leaseID)
	case <-time.After(20 * time.Millisecond):
	}
	duplicateCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if duplicate, open := f.Take(duplicateCtx); open {
		t.Fatalf("redelivered order seeded duplicate job %q", duplicate.URL)
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
