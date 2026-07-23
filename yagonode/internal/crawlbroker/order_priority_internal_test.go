package crawlbroker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func automaticOrder(name string) yagocrawlcontract.CrawlOrder {
	order := testOrder(name)
	order.Priority = yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery

	return order
}

func leaseOrderName(t *testing.T, queue *DurableOrderQueue) string {
	t.Helper()
	data, _, ok, err := queue.leasePop(t.Context(), "worker")
	if err != nil || !ok {
		t.Fatalf("lease: ok=%v err=%v", ok, err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
	if err != nil {
		t.Fatalf("decode leased order: %v", err)
	}

	return order.Profile.Name
}

func publishOrders(t *testing.T, queue *DurableOrderQueue, orders ...yagocrawlcontract.CrawlOrder) {
	t.Helper()
	for _, order := range orders {
		if err := queue.Publish(t.Context(), order); err != nil {
			t.Fatalf("publish %s: %v", order.Profile.Name, err)
		}
	}
}

func TestAutomaticDiscoveryOrdersLeadNormalOrdersByDefault(t *testing.T) {
	queue := memQueue(t)
	publishOrders(t, queue, testOrder("normal"), automaticOrder("automatic"))

	for _, want := range []string{"automatic", "normal"} {
		if got := leaseOrderName(t, queue); got != want {
			t.Fatalf("leased %q, want %q", got, want)
		}
	}
}

func TestAutomaticDiscoveryPriorityGuaranteesNormalService(t *testing.T) {
	queue := memQueue(t)
	publishOrders(t, queue,
		testOrder("normal-1"), testOrder("normal-2"),
		automaticOrder("automatic-1"), automaticOrder("automatic-2"),
		automaticOrder("automatic-3"), automaticOrder("automatic-4"),
		automaticOrder("automatic-5"),
	)

	want := []string{
		"automatic-1", "automatic-2", "automatic-3", "normal-1",
		"automatic-4", "automatic-5", "normal-2",
	}
	for _, name := range want {
		if got := leaseOrderName(t, queue); got != name {
			t.Fatalf("leased %q, want %q in %v", got, name, want)
		}
	}
}

func TestDisabledAutomaticDiscoveryPriorityRestoresGlobalFIFO(t *testing.T) {
	queue := memQueue(t)
	queue.SetAutomaticDiscoveryPriority(false)
	publishOrders(t, queue,
		testOrder("normal-1"), automaticOrder("automatic-1"),
		testOrder("normal-2"), automaticOrder("automatic-2"),
	)

	for _, want := range []string{"normal-1", "automatic-1", "normal-2", "automatic-2"} {
		if got := leaseOrderName(t, queue); got != want {
			t.Fatalf("leased %q, want %q", got, want)
		}
	}
}

func TestDisabledAutomaticDiscoveryPriorityLeasesNormalOnlyQueue(t *testing.T) {
	queue := memQueue(t)
	queue.SetAutomaticDiscoveryPriority(false)
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "normal" {
		t.Fatalf("leased %q, want normal", got)
	}
}

func TestAutomaticDiscoveryBurstSurvivesQueueReopen(t *testing.T) {
	engine := newScriptedEngine()
	firstVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("first vault: %v", err)
	}
	first, err := newDurableOrderQueue(firstVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("first queue: %v", err)
	}
	publishOrders(t, first,
		testOrder("normal"), automaticOrder("automatic-1"),
		automaticOrder("automatic-2"), automaticOrder("automatic-3"),
	)
	for _, want := range []string{"automatic-1", "automatic-2"} {
		if got := leaseOrderName(t, first); got != want {
			t.Fatalf("before reopen leased %q, want %q", got, want)
		}
	}

	secondVault, err := vault.New(engine)
	if err != nil {
		t.Fatalf("second vault: %v", err)
	}
	second, err := newDurableOrderQueue(secondVault, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("second queue: %v", err)
	}
	for _, want := range []string{"automatic-3", "normal"} {
		if got := leaseOrderName(t, second); got != want {
			t.Fatalf("after reopen leased %q, want %q", got, want)
		}
	}
}

func TestAutomaticDiscoveryPrioritySurvivesDeferredLease(t *testing.T) {
	set := withClock(t)
	base := time.Unix(900, 0)
	set(base)
	queue := memQueue(t)
	publishOrders(t, queue, automaticOrder("automatic"))
	_, leaseID, ok, err := queue.leasePop(context.Background(), "worker")
	if err != nil || !ok {
		t.Fatalf("lease automatic: ok=%v err=%v", ok, err)
	}
	if err := queue.deferLease(context.Background(), leaseID); err != nil {
		t.Fatalf("defer automatic: %v", err)
	}
	set(base.Add(negativeAcknowledgmentRetryDelay))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("release automatic: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "automatic" {
		t.Fatalf("leased %q, want deferred automatic order", got)
	}
}

func TestAutomaticDiscoveryPriorityRecoversFromLegacyLeasePayload(t *testing.T) {
	set := withClock(t)
	base := time.Unix(900, 0)
	set(base)
	queue := memQueue(t)
	publishOrders(t, queue, automaticOrder("automatic"))
	_, leaseID, ok, err := queue.leasePop(context.Background(), "worker")
	if err != nil || !ok {
		t.Fatalf("lease automatic: ok=%v err=%v", ok, err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read lease: %w", err)
		}
		if !found {
			return fmt.Errorf("lease %s not found", leaseID)
		}
		record.Priority = yagocrawlcontract.CrawlOrderPriorityNormal
		if err := queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("store legacy lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("prepare legacy lease: %v", err)
	}
	if err := queue.deferLease(context.Background(), leaseID); err != nil {
		t.Fatalf("defer legacy lease: %v", err)
	}
	set(base.Add(negativeAcknowledgmentRetryDelay))
	if err := queue.sweepExpired(context.Background()); err != nil {
		t.Fatalf("release legacy lease: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "automatic" {
		t.Fatalf("leased %q, want automatic priority recovered from payload", got)
	}
}

func TestAutomaticDiscoveryPrioritySurvivesExpiredLeaseRequeue(t *testing.T) {
	set := withClock(t)
	base := time.Unix(1000, 0)
	set(base)
	queue := memQueue(t)
	publishOrders(t, queue, automaticOrder("automatic"))
	_, _, ok, err := queue.leasePop(t.Context(), "worker")
	if err != nil || !ok {
		t.Fatalf("lease automatic: ok=%v err=%v", ok, err)
	}
	set(base.Add(DefaultLeaseTTL))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("requeue expired leases: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "automatic" {
		t.Fatalf("leased %q, want requeued automatic order", got)
	}
}

func TestAutomaticDiscoveryPriorityRecoversDuringLegacyExpiredLeaseRequeue(t *testing.T) {
	queue := memQueue(t)
	data, err := yagocrawlcontract.MarshalCrawlOrder(automaticOrder("automatic"))
	if err != nil {
		t.Fatalf("marshal automatic order: %v", err)
	}
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		if err := queue.leases.Put(tx, vault.Key("legacy-lease"), leaseRecord{
			OrderData: data,
			WorkerID:  "legacy-worker",
		}); err != nil {
			return fmt.Errorf("store legacy lease: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("prepare legacy lease: %v", err)
	}
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("requeue legacy expired lease: %v", err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "automatic" {
		t.Fatalf("leased %q, want automatic priority recovered during requeue", got)
	}
}

func TestAutomaticDiscoveryUsesSeparateIdempotencyNamespace(t *testing.T) {
	queue := memQueue(t)
	duplicate, err := queue.PublishOnce(t.Context(), "seed", automaticOrder("automatic"))
	if err != nil || duplicate {
		t.Fatalf("publish automatic: duplicate=%v err=%v", duplicate, err)
	}
	duplicate, err = queue.PublishOnce(t.Context(), "seed", testOrder("duplicate"))
	if err != nil || duplicate {
		t.Fatalf("publish manual namespace: duplicate=%v err=%v", duplicate, err)
	}
	publishOrders(t, queue, testOrder("normal"))
	if got := leaseOrderName(t, queue); got != "automatic" {
		t.Fatalf("leased %q, want original automatic order", got)
	}
}
