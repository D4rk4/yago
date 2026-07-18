package crawlbroker

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestWorkerSessionRegistryRejectsAbsentAndStaleRegistrations(t *testing.T) {
	registry := newWorkerSessionRegistry(2)
	if _, err := registry.activate("", "", func() {}, func() error { return nil }); err == nil {
		t.Fatal("invalid worker session activated")
	}
	registry.deactivate("missing", "session", 1)
	if err := registry.whileCurrentRegistration(
		"missing",
		"session",
		1,
		func() error { return nil },
	); !errors.Is(err, errLeaseLost) {
		t.Fatalf("missing registration error = %v", err)
	}
	generation, err := registry.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate worker: %v", err)
	}
	if err := registry.whileCurrentRegistration(
		"worker",
		"session",
		generation+1,
		func() error { return nil },
	); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stale generation error = %v", err)
	}
	if err := registry.whileCurrent(
		"missing",
		"session",
		func() error { return nil },
	); !errors.Is(err, errLeaseLost) {
		t.Fatalf("missing current session error = %v", err)
	}
	registry.deactivate("worker", "session", generation)
	called := false
	if err := registry.whileCurrent("worker", "session", func() error {
		called = true

		return nil
	}); err != nil || !called {
		t.Fatalf("disconnected retained session called=%v error=%v", called, err)
	}
	if current := registry.registration("missing"); current.id != "" || current.cancel != nil ||
		current.connected || !current.lastSeen.IsZero() || current.generation != 0 {
		t.Fatalf("missing registration = %#v", current)
	}
}

func TestRecoveredOrderAndLeaseErrorsFenceStaleSession(t *testing.T) {
	server := newExchangeServer(memQueue(t), nil)
	err := server.streamRecoveredOrders(
		nil,
		"missing",
		"session",
		1,
		[]leasedCrawlOrder{{LeaseID: "lease", OrderData: []byte("order")}},
	)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("recovered order status = %v", status.Code(err))
	}
	if err := streamLeaseError(
		context.Background(),
		context.Background(),
		errLeaseLost,
	); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("lease-lost stream status = %v", status.Code(err))
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := streamLeaseError(
		cancelled,
		context.Background(),
		errors.New("session stopped"),
	); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("cancelled session status = %v", status.Code(err))
	}
}

func TestTerminalProgressWithoutOwnedRunIsIgnored(t *testing.T) {
	server := newExchangeServer(memQueue(t), nil)
	if err := server.recordCurrentProgress(t.Context(), yagocrawlcontract.CrawlRunProgress{
		WorkerID: "worker",
		RunID:    "missing",
		State:    yagocrawlcontract.CrawlRunFinished,
	}); err != nil {
		t.Fatalf("unowned terminal progress error = %v", err)
	}
}
