package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestOrderAdmissionUsesRunLimit(t *testing.T) {
	admission := newOrderAdmission()
	if !admission.SetMaximumActiveRuns(1) {
		t.Fatal("valid limits were rejected")
	}
	if err := admission.Acquire(context.Background()); err != nil {
		t.Fatalf("first order admission: %v", err)
	}
	defer admission.Release()

	blockedContext, cancelBlocked := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelBlocked()
	if err := admission.Acquire(blockedContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second order admission error = %v", err)
	}

	if !admission.SetMaximumActiveRuns(2) {
		t.Fatal("raised active run limit was rejected")
	}
	availableContext, cancelAvailable := context.WithTimeout(context.Background(), time.Second)
	defer cancelAvailable()
	if err := admission.Acquire(availableContext); err != nil {
		t.Fatalf("order admission after raising limit: %v", err)
	}
	defer admission.Release()
}

func TestOrderAdmissionRejectsInvalidLimitsWithoutChangingCapacity(t *testing.T) {
	admission := newOrderAdmission()
	if !admission.SetMaximumActiveRuns(1) {
		t.Fatal("valid baseline limits were rejected")
	}
	if admission.SetMaximumActiveRuns(0) ||
		admission.SetMaximumActiveRuns(yagocrawlcontract.MaximumActiveCrawlRunConcurrency+1) {
		t.Fatal("invalid limits were accepted")
	}
	if err := admission.Acquire(context.Background()); err != nil {
		t.Fatalf("first order admission: %v", err)
	}
	defer admission.Release()
	blockedContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := admission.Acquire(blockedContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("admission after invalid limits error = %v", err)
	}
}
