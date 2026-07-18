package crawlorder

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestActiveOrdersRejectsConflictingLiveDefinitions(t *testing.T) {
	firstIdentity := sha256.Sum256([]byte("first-payload"))
	otherIdentity := sha256.Sum256([]byte("other-payload"))
	cases := []struct {
		name     string
		conflict func(CrawlOrderDelivery) CrawlOrderDelivery
	}{
		{
			name: "exact identity",
			conflict: func(delivery CrawlOrderDelivery) CrawlOrderDelivery {
				delivery.OrderIdentity = otherIdentity[:]

				return delivery
			},
		},
		{
			name: "priority",
			conflict: func(delivery CrawlOrderDelivery) CrawlOrderDelivery {
				delivery.Order.Priority = yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery

				return delivery
			},
		},
		{
			name: "payload",
			conflict: func(delivery CrawlOrderDelivery) CrawlOrderDelivery {
				delivery.Order.Requests = append(
					[]yagocrawlcontract.CrawlRequest(nil),
					delivery.Order.Requests...,
				)
				delivery.Order.Requests[0].URL = "https://example.com/conflict"

				return delivery
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			active := newActiveOrders()
			order := identityTestOrder()
			first := CrawlOrderDelivery{
				LeaseID:       "first-lease",
				Order:         order,
				OrderIdentity: firstIdentity[:],
			}
			if claim := active.claim(order.Provenance, first); claim != activeOrderStartsRun {
				t.Fatalf("first claim = %d, want start", claim)
			}
			conflict := test.conflict(CrawlOrderDelivery{
				LeaseID:       "conflicting-lease",
				Order:         order,
				OrderIdentity: firstIdentity[:],
			})
			if claim := active.claim(order.Provenance, conflict); claim != activeOrderRejected {
				t.Fatalf("conflicting claim = %d, want rejected", claim)
			}
			var settled []string
			if !active.settle(
				order.Provenance,
				first,
				false,
				func(delivery CrawlOrderDelivery) bool {
					settled = append(settled, delivery.LeaseID)

					return true
				},
			) {
				t.Fatal("original delivery was not settled")
			}
			if len(settled) != 1 || settled[0] != first.LeaseID {
				t.Fatalf("settled leases = %v, want [%s]", settled, first.LeaseID)
			}
		})
	}
}

func TestActiveOrdersRejectsInvalidDefinition(t *testing.T) {
	active := newActiveOrders()
	order := identityTestOrder()
	if claim := active.claim(order.Provenance, CrawlOrderDelivery{
		Order:         order,
		OrderIdentity: []byte("short"),
	}); claim != activeOrderRejected {
		t.Fatalf("invalid exact identity claim = %d, want rejected", claim)
	}

	saved := marshalCrawlOrderIdentity
	t.Cleanup(func() { marshalCrawlOrderIdentity = saved })
	marshalCrawlOrderIdentity = func(yagocrawlcontract.CrawlOrder) ([]byte, error) {
		return nil, errors.New("marshal unavailable")
	}
	exactIdentity := sha256.Sum256([]byte("valid-exact-identity"))
	if claim := active.claim(order.Provenance, CrawlOrderDelivery{
		Order:         order,
		OrderIdentity: exactIdentity[:],
	}); claim != activeOrderRejected {
		t.Fatalf("unverifiable payload claim = %d, want rejected", claim)
	}
	if len(active.deliveries) != 0 {
		t.Fatalf("invalid definitions created %d active deliveries", len(active.deliveries))
	}
}

func TestConsumerTerminatesConflictingDeliveryWithoutReplacingActiveRun(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(2, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	)
	order := identityTestOrder()
	identity := sha256.Sum256([]byte("stable-wire-order"))
	acknowledged := make(chan struct{})
	consumer.accept(t.Context(), CrawlOrderDelivery{
		LeaseID:       "original-lease",
		Order:         order,
		OrderIdentity: identity[:],
		Ack: func(context.Context) error {
			close(acknowledged)

			return nil
		},
	})
	conflictingOrder := order
	conflictingOrder.Requests = append([]yagocrawlcontract.CrawlRequest(nil), order.Requests...)
	conflictingOrder.Requests[0].URL = "https://example.com/conflicting"
	terminated := make(chan struct{})
	consumer.accept(t.Context(), CrawlOrderDelivery{
		LeaseID:       "conflicting-lease",
		Order:         conflictingOrder,
		OrderIdentity: identity[:],
		Term: func(context.Context) error {
			close(terminated)

			return nil
		},
	})
	waitCallback(t, terminated)
	job, ok := crawlFrontier.Take(t.Context())
	if !ok {
		t.Fatal("frontier closed before original job")
	}
	if job.URL != order.Requests[0].URL {
		t.Fatalf("dispatched URL = %q, want %q", job.URL, order.Requests[0].URL)
	}
	crawlFrontier.Done(job, successfulPageOutcome())
	waitCallback(t, acknowledged)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if duplicate, open := crawlFrontier.Take(ctx); open {
		t.Fatalf("conflicting delivery dispatched %q", duplicate.URL)
	}
}
