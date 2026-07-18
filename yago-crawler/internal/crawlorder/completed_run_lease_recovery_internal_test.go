package crawlorder

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestActiveOrdersCompletedRunRecoveryInstallsReplacementDelivery(t *testing.T) {
	active := newActiveOrders()
	provenance := []byte("completed-run-replacement")
	previous := CrawlOrderDelivery{LeaseID: "previous-lease"}
	replacement := CrawlOrderDelivery{LeaseID: "replacement-lease"}
	if claim := active.claim(provenance, previous); claim != activeOrderStartsRun {
		t.Fatalf("previous claim = %d", claim)
	}
	claim := active.claim(
		provenance,
		replacement,
		func(previousLeaseID string, replacementLeaseID string) activeOrderClaim {
			if previousLeaseID != previous.LeaseID || replacementLeaseID != replacement.LeaseID {
				t.Fatalf("lease rebind = %q -> %q", previousLeaseID, replacementLeaseID)
			}

			return activeOrderRecoversCompletedRun
		},
	)
	if claim != activeOrderRecoversCompletedRun {
		t.Fatalf("replacement claim = %d", claim)
	}
	settledLeaseID := ""
	if !active.settleDurably(
		provenance,
		replacement,
		true,
		func(delivery CrawlOrderDelivery) bool {
			settledLeaseID = delivery.LeaseID

			return true
		},
	) {
		t.Fatal("replacement settlement failed")
	}
	if settledLeaseID != replacement.LeaseID {
		t.Fatalf("settled lease = %q", settledLeaseID)
	}
}

func TestReplacementLeaseRichSettlesCompletedCheckpointBeforeOldSettlementWins(
	t *testing.T,
) {
	fixture := newCompletedLeaseReplacement(t)
	previous := fixture.previousDelivery()
	fixture.consumer.accept(t.Context(), previous)
	job, open := fixture.crawlFrontier.Take(t.Context())
	if !open || job.LeaseID != previous.LeaseID {
		t.Fatalf("completed replacement job = %+v/%t", job, open)
	}
	fixture.crawlFrontier.Done(job, yagocrawlcontract.CrawlRunTally{Fetched: 1})
	waitCompletedLeaseSignal(
		t,
		fixture.oldSettlementStarted,
		"old terminal settlement did not start",
	)
	replacement := fixture.replacementDelivery()
	fixture.consumer.accept(t.Context(), replacement)
	fixture.expectReplacementSettlement(t)
	select {
	case leaseID := <-fixture.legacyAcknowledgment:
		t.Fatalf("replacement used legacy acknowledgment %q", leaseID)
	default:
	}
	close(fixture.releaseOldSettlement)
	waitCompletedLeaseSignal(
		t,
		fixture.oldSettlementReturned,
		"old terminal settlement did not return",
	)
	fixture.crawlFrontier.WaitForSettlements()
	fixture.expectCompletedLeases(t, previous.LeaseID, replacement.LeaseID)
}

type completedLeaseReplacement struct {
	consumer              *CrawlOrderConsumer
	crawlFrontier         *frontier.Frontier
	order                 yagocrawlcontract.CrawlOrder
	identity              []byte
	oldSettlementStarted  chan struct{}
	releaseOldSettlement  chan struct{}
	oldSettlementReturned chan struct{}
	replacementSettlement chan terminalRunSettlement
	legacyAcknowledgment  chan string
}

func newCompletedLeaseReplacement(t *testing.T) *completedLeaseReplacement {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier.db"))
	if err != nil {
		t.Fatalf("open completed replacement checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	crawlFrontier := frontier.NewFrontier(4, nil, frontier.WithCheckpoint(checkpoint))
	consumer := NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](2),
		crawlFrontier,
	)
	profile := consumerProfile()
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("completed-checkpoint-replacement"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.org/completed-replacement",
			ProfileHandle: profile.Handle,
		}},
	}
	payload, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal completed replacement order: %v", err)
	}
	identity := sha256.Sum256(payload)

	return &completedLeaseReplacement{
		consumer:              consumer,
		crawlFrontier:         crawlFrontier,
		order:                 order,
		identity:              identity[:],
		oldSettlementStarted:  make(chan struct{}),
		releaseOldSettlement:  make(chan struct{}),
		oldSettlementReturned: make(chan struct{}),
		replacementSettlement: make(chan terminalRunSettlement, 1),
		legacyAcknowledgment:  make(chan string, 2),
	}
}

func (fixture *completedLeaseReplacement) previousDelivery() CrawlOrderDelivery {
	return CrawlOrderDelivery{
		LeaseID:       "completed-old-lease",
		Order:         fixture.order,
		OrderIdentity: fixture.identity,
		Ack: func(context.Context) error {
			fixture.legacyAcknowledgment <- "old"

			return nil
		},
		settleTerminal: func(context.Context, terminalRunSettlement) error {
			close(fixture.oldSettlementStarted)
			<-fixture.releaseOldSettlement
			close(fixture.oldSettlementReturned)

			return errors.New("old lease lost")
		},
	}
}

func (fixture *completedLeaseReplacement) replacementDelivery() CrawlOrderDelivery {
	return CrawlOrderDelivery{
		LeaseID:       "completed-new-lease",
		Order:         fixture.order,
		OrderIdentity: fixture.identity,
		Ack: func(context.Context) error {
			fixture.legacyAcknowledgment <- "new"

			return nil
		},
		settleTerminal: func(
			_ context.Context,
			settlement terminalRunSettlement,
		) error {
			fixture.replacementSettlement <- settlement

			return nil
		},
	}
}

func (fixture *completedLeaseReplacement) expectReplacementSettlement(t *testing.T) {
	t.Helper()
	select {
	case settlement := <-fixture.replacementSettlement:
		if settlement.Disposition != crawlOrderAcknowledged ||
			settlement.State != yagocrawlcontract.CrawlRunFinished ||
			settlement.Tally.Fetched != 1 || settlement.Tally.Pending != 0 {
			t.Fatalf("replacement terminal settlement = %+v", settlement)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement lease did not rich-settle completed checkpoint")
	}
}

func waitCompletedLeaseSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func (fixture *completedLeaseReplacement) expectCompletedLeases(
	t *testing.T,
	previousLeaseID string,
	replacementLeaseID string,
) {
	t.Helper()
	fixture.consumer.active.mu.Lock()
	_, replacementCompleted := fixture.consumer.active.completedLeases[replacementLeaseID]
	_, previousCompleted := fixture.consumer.active.completedLeases[previousLeaseID]
	fixture.consumer.active.mu.Unlock()
	if !replacementCompleted || previousCompleted {
		t.Fatalf(
			"completed lease retention old/new = %t/%t",
			previousCompleted,
			replacementCompleted,
		)
	}
}
