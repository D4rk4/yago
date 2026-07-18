package main

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestWorkerConcurrencyCoalescesChanges(t *testing.T) {
	control := newWorkerConcurrency(yagocrawlcontract.DefaultFetchWorkerConcurrency)
	control.Set(8)
	control.Set(12)
	control.Set(12)
	if got := control.Current(); got != 12 {
		t.Fatalf("current workers = %d, want 12", got)
	}
	select {
	case <-control.Changes():
	default:
		t.Fatal("worker change was not signalled")
	}
	select {
	case <-control.Changes():
		t.Fatal("worker changes were not coalesced")
	default:
	}
	control.Set(0)
	if got := control.Current(); got != 12 {
		t.Fatalf("zero changed workers to %d", got)
	}
	control.DrainChanges()
	var nilControl *workerConcurrency
	if nilControl.Current() != 0 || nilControl.Changes() != nil {
		t.Fatal("nil concurrency control did not stay inert")
	}
	nilControl.Set(4)
	nilControl.DrainChanges()
}
