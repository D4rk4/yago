package crawlorder

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestReplacementLeaseRebindsActiveRunWithoutStaleAdmissionLoop(t *testing.T) {
	tests := []struct {
		name   string
		revoke bool
	}{
		{name: "revoke", revoke: true},
		{name: "expiry"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runReplacementLeaseRebinding(t, test.name, test.revoke)
		})
	}
}

func runReplacementLeaseRebinding(t *testing.T, name string, revoke bool) {
	t.Helper()
	crawlFrontier := frontier.NewFrontier(4, nil)
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	)
	profile := consumerProfile()
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("lease-rebind-" + name),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.org/",
			ProfileHandle: profile.Handle,
		}},
	}
	settled := make(chan string, 2)
	delivery := leaseRebindingDelivery(order, settled)
	registry := crawllease.NewGrantRegistry(t.Context(), 2)
	oldLeaseID := "old-lease-" + name
	oldLifetime := time.Hour
	if !revoke {
		oldLifetime = 10 * time.Millisecond
	}
	oldGrant := confirmRebindingLease(t, registry, oldLeaseID, oldLifetime)
	consumer.accept(t.Context(), delivery(oldLeaseID))
	oldJob := takeRebindingJob(t, crawlFrontier, oldLeaseID)
	loseRebindingLease(t, registry, oldLeaseID, oldGrant.Done(), revoke)
	newLeaseID := "new-lease-" + name
	confirmRebindingLease(t, registry, newLeaseID, time.Hour)
	consumer.accept(t.Context(), delivery(newLeaseID))
	crawlFrontier.Done(oldJob, successfulPageOutcome())
	expectNoRebindingSettlement(t, settled, 0)
	attempts, replacementLeaseID := finishLiveRebindingJob(t, crawlFrontier, registry)
	if attempts != 1 || replacementLeaseID != newLeaseID {
		t.Fatalf(
			"replacement admission attempts/lease = %d/%q, want 1/%q",
			attempts,
			replacementLeaseID,
			newLeaseID,
		)
	}
	expectRebindingSettlement(t, settled, newLeaseID)
	expectNoRebindingSettlement(t, settled, 20*time.Millisecond)
}

func leaseRebindingDelivery(
	order yagocrawlcontract.CrawlOrder,
	settled chan<- string,
) func(string) CrawlOrderDelivery {
	return func(leaseID string) CrawlOrderDelivery {
		return CrawlOrderDelivery{
			LeaseID: leaseID,
			Order:   order,
			Ack: func(context.Context) error {
				settled <- leaseID

				return nil
			},
			Nak: func(context.Context) error {
				settled <- "nak:" + leaseID

				return nil
			},
		}
	}
}

func confirmRebindingLease(
	t *testing.T,
	registry *crawllease.GrantRegistry,
	leaseID string,
	lifetime time.Duration,
) context.Context {
	t.Helper()
	if err := registry.Track(leaseID); err != nil {
		t.Fatal(err)
	}
	registry.Renew(time.Now(), lifetime, []string{leaseID}, []string{leaseID})
	grant, confirmed := registry.Context(leaseID)
	if !confirmed {
		t.Fatalf("lease %q was not confirmed", leaseID)
	}

	return grant
}

func takeRebindingJob(
	t *testing.T,
	crawlFrontier *frontier.Frontier,
	leaseID string,
) crawljob.CrawlJob {
	t.Helper()
	job, open := crawlFrontier.Take(t.Context())
	if !open || job.LeaseID != leaseID {
		t.Fatalf("job = %+v/%t, want lease %q", job, open, leaseID)
	}

	return job
}

func loseRebindingLease(
	t *testing.T,
	registry *crawllease.GrantRegistry,
	leaseID string,
	expired <-chan struct{},
	revoke bool,
) {
	t.Helper()
	if revoke {
		registry.Revoke(leaseID)

		return
	}
	select {
	case <-expired:
	case <-time.After(time.Second):
		t.Fatal("old lease did not expire")
	}
}

func finishLiveRebindingJob(
	t *testing.T,
	crawlFrontier *frontier.Frontier,
	registry *crawllease.GrantRegistry,
) (int, string) {
	t.Helper()
	for attempt := 1; attempt <= 4; attempt++ {
		job, open := crawlFrontier.Take(t.Context())
		if !open {
			t.Fatal("frontier closed before replacement job")
		}
		if _, live := registry.Context(job.LeaseID); live {
			crawlFrontier.Done(job, successfulPageOutcome())

			return attempt, job.LeaseID
		}
		crawlFrontier.Abandon(job)
	}

	return 4, ""
}

func expectRebindingSettlement(t *testing.T, settled <-chan string, want string) {
	t.Helper()
	select {
	case disposition := <-settled:
		if disposition != want {
			t.Fatalf("settled lease = %q, want %q", disposition, want)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement lease was not settled")
	}
}

func expectNoRebindingSettlement(t *testing.T, settled <-chan string, wait time.Duration) {
	t.Helper()
	if wait == 0 {
		select {
		case disposition := <-settled:
			t.Fatalf("unexpected settlement = %q", disposition)
		default:
		}

		return
	}
	select {
	case disposition := <-settled:
		t.Fatalf("unexpected settlement = %q", disposition)
	case <-time.After(wait):
	}
}
