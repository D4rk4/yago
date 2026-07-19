package crawlbroker

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestControlRegistryConvergesProcessFetchRate(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{
		processPagesPerSecond: 10,
		processRateSet:        true,
	})
	registry.register("crawler-a")
	initial, err := registry.deliverForHeartbeat(t.Context(), "crawler-a", nil)
	if err != nil {
		t.Fatalf("initial heartbeat: %v", err)
	}
	if !hasProcessRate(initial, 10) {
		t.Fatalf("initial directives = %+v, want process rate 10", initial)
	}
	if signalled := registry.SetProcessPagesPerSecond(27); signalled != 1 {
		t.Fatalf("signalled workers = %d, want 1", signalled)
	}
	updated, err := registry.deliverForHeartbeat(t.Context(), "crawler-a", nil)
	if err != nil {
		t.Fatalf("updated heartbeat: %v", err)
	}
	if !hasProcessRate(updated, 27) || registry.ProcessPagesPerSecond() != 27 {
		t.Fatalf("updated directives/rate = %+v/%d", updated,
			registry.ProcessPagesPerSecond())
	}
	if signalled := registry.SetProcessPagesPerSecond(0); signalled != 1 {
		t.Fatalf("unlimited signal count = %d, want 1", signalled)
	}
	unlimited, err := registry.deliverForHeartbeat(t.Context(), "crawler-a", nil)
	if err != nil {
		t.Fatalf("unlimited heartbeat: %v", err)
	}
	if !hasProcessRate(unlimited, 0) {
		t.Fatalf("unlimited directive = %+v", unlimited)
	}
	for _, invalid := range []int{-1, yagocrawlcontract.MaximumProcessPagesPerSecond + 1} {
		if signalled := registry.SetProcessPagesPerSecond(invalid); signalled != 0 {
			t.Fatalf("invalid rate %d signalled %d workers", invalid, signalled)
		}
	}
}

func TestControlRegistrySerializesProcessFetchRateChanges(t *testing.T) {
	registry := newControlRegistry()
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	var ratesMutex sync.Mutex
	rates := make([]uint32, 0, 2)
	registry.bindFleetFetchStarts(func(rate uint32) error {
		ratesMutex.Lock()
		rates = append(rates, rate)
		ratesMutex.Unlock()
		if rate == 11 {
			close(firstEntered)
			<-releaseFirst
		} else {
			close(secondEntered)
		}

		return nil
	})
	firstDone := make(chan int, 1)
	go func() { firstDone <- registry.SetProcessPagesPerSecond(11) }()
	<-firstEntered
	secondDone := make(chan int, 1)
	go func() { secondDone <- registry.SetProcessPagesPerSecond(22) }()
	select {
	case <-secondEntered:
		t.Fatal("second process rate reached the fleet before the first completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	if signalled := <-firstDone; signalled != 0 {
		t.Fatalf("first signal count = %d", signalled)
	}
	if signalled := <-secondDone; signalled != 0 {
		t.Fatalf("second signal count = %d", signalled)
	}
	ratesMutex.Lock()
	gotRates := append([]uint32(nil), rates...)
	ratesMutex.Unlock()
	if !slices.Equal(gotRates, []uint32{11, 22}) || registry.ProcessPagesPerSecond() != 22 {
		t.Fatalf("fleet rates/final rate = %v/%d", gotRates, registry.ProcessPagesPerSecond())
	}
}

func TestProcessFetchRateChangeDoesNotInvertHeartbeatSessionLock(t *testing.T) {
	registry := newControlRegistry()
	sessions := newWorkerSessionRegistry(1)
	if _, err := sessions.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
		true,
	); err != nil {
		t.Fatalf("activate worker session: %v", err)
	}
	policyEntered := make(chan struct{})
	registry.bindFleetFetchStarts(func(uint32) error {
		close(policyEntered)
		sessions.disconnectActiveSessions()

		return nil
	})
	sessionHeld := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- sessions.whileCurrent("worker", "session", func() error {
			close(sessionHeld)
			<-policyEntered
			activeFetches := uint32(1)
			registry.recordActiveFetches("worker", &activeFetches)

			return nil
		})
	}()
	<-sessionHeld
	rateDone := make(chan int, 1)
	go func() { rateDone <- registry.SetProcessPagesPerSecond(10) }()
	select {
	case err := <-heartbeatDone:
		if err != nil {
			t.Fatalf("heartbeat operation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat deadlocked behind process rate change")
	}
	select {
	case signalled := <-rateDone:
		if signalled != 0 || registry.ProcessPagesPerSecond() != 10 {
			t.Fatalf("rate signal/final rate = %d/%d", signalled, registry.ProcessPagesPerSecond())
		}
	case <-time.After(time.Second):
		t.Fatal("process rate change did not finish after heartbeat released its session")
	}
}

func hasProcessRate(
	directives []yagocrawlcontract.CrawlControlDirective,
	rate uint32,
) bool {
	for _, directive := range directives {
		if directive.Kind == yagocrawlcontract.CrawlControlSetProcessRate &&
			directive.ProcessPagesPerSecond == rate {
			return true
		}
	}

	return false
}
