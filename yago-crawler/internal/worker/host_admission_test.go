package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostAdmissionLimitsOneHostWithoutBlockingAnother(t *testing.T) {
	admission := newHostAdmission(0, 1)
	releaseFirst, err := admission.Acquire(context.Background(), "Example.test")
	if err != nil {
		t.Fatalf("first host admission: %v", err)
	}

	blockedContext, cancelBlocked := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelBlocked()
	if _, err := admission.Acquire(blockedContext, "example.test"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("duplicate host admission error = %v", err)
	}

	releaseOther, err := admission.Acquire(context.Background(), "other.test")
	if err != nil {
		t.Fatalf("independent host admission: %v", err)
	}
	releaseOther()

	releaseFirst()
	releaseAgain, err := admission.Acquire(context.Background(), "example.test")
	if err != nil {
		t.Fatalf("host admission after release: %v", err)
	}
	releaseAgain()
}

func TestHostAdmissionAppliesDelayBetweenStarts(t *testing.T) {
	admission := newHostAdmission(40*time.Millisecond, 2)
	releaseFirst, err := admission.Acquire(context.Background(), "example.test")
	if err != nil {
		t.Fatalf("first host admission: %v", err)
	}
	releaseFirst()
	started := time.Now()
	releaseAgain, err := admission.Acquire(context.Background(), "example.test")
	if err != nil {
		t.Fatalf("second host admission: %v", err)
	}
	releaseAgain()
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("host start delay = %s", elapsed)
	}
}

func TestHostAdmissionRejectsEmptyHost(t *testing.T) {
	if _, err := newHostAdmission(0, 1).Acquire(context.Background(), " "); err == nil {
		t.Fatal("empty host was admitted")
	}
}
