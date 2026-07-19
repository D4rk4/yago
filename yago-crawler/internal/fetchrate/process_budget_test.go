package fetchrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestProcessBudgetSpacesConcurrentAdmissions(t *testing.T) {
	budget := NewProcessBudget(20)
	if err := budget.Wait(t.Context()); err != nil {
		t.Fatalf("first admission: %v", err)
	}
	started := time.Now()
	done := make(chan time.Time, 2)
	for range 2 {
		go func() {
			if err := budget.Wait(t.Context()); err != nil {
				done <- time.Time{}

				return
			}
			done <- time.Now()
		}()
	}
	first := <-done
	second := <-done
	if first.IsZero() || second.IsZero() {
		t.Fatal("concurrent admission failed")
	}
	if second.Before(first) {
		first, second = second, first
	}
	if first.Sub(started) < 40*time.Millisecond || second.Sub(first) < 40*time.Millisecond {
		t.Fatalf("admissions were not process-paced: first=%s gap=%s",
			first.Sub(started), second.Sub(first))
	}
}

func TestProcessBudgetAppliesLiveChangesAndCancellation(t *testing.T) {
	budget := NewProcessBudget(1)
	if err := budget.Wait(t.Context()); err != nil {
		t.Fatalf("first admission: %v", err)
	}
	cancelled, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := budget.Wait(cancelled); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled wait = %v, want deadline", err)
	}

	released := make(chan error, 1)
	go func() { released <- budget.Wait(t.Context()) }()
	time.Sleep(20 * time.Millisecond)
	budget.Set(0)
	select {
	case err := <-released:
		if err != nil {
			t.Fatalf("unlimited wait: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("live unlimited rate did not release waiter")
	}
	if budget.PagesPerSecond() != 0 {
		t.Fatalf("rate = %d, want unlimited", budget.PagesPerSecond())
	}
	budget.Set(0)
	budget.Set(yagocrawlcontract.MaximumProcessPagesPerSecond + 1)
	if budget.PagesPerSecond() != 0 {
		t.Fatal("invalid live rate was applied")
	}
}

func TestProcessBudgetReportsContextLiveWaitingDemand(t *testing.T) {
	budget := NewProcessBudget(1)
	if budget.Waiting() != 0 {
		t.Fatalf("initial waiting demand = %d", budget.Waiting())
	}
	if err := budget.Wait(t.Context()); err != nil {
		t.Fatalf("prime budget: %v", err)
	}
	waitContext, cancelWait := context.WithCancel(t.Context())
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- budget.Wait(waitContext) }()
	}
	deadline := time.Now().Add(time.Second)
	for budget.Waiting() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("waiting demand = %d, want 2", budget.Waiting())
		}
		time.Sleep(time.Millisecond)
	}
	cancelWait()
	for range 2 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel waiting demand: %v", err)
		}
	}
	if budget.Waiting() != 0 {
		t.Fatalf("released waiting demand = %d", budget.Waiting())
	}
}
