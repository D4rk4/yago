package crawlorder

import (
	"context"
	"testing"
	"time"
)

type activeRunAcquisition struct {
	release  func()
	admitted bool
}

func TestActiveRunAdmissionBoundsAndReleasesOnce(t *testing.T) {
	admission := NewActiveRunAdmission(2)
	first, admitted := admission.acquire(t.Context())
	if !admitted {
		t.Fatal("first run was not admitted")
	}
	second, admitted := admission.acquire(t.Context())
	if !admitted {
		t.Fatal("second run was not admitted")
	}
	third := make(chan activeRunAcquisition, 1)
	go func() {
		release, ok := admission.acquire(t.Context())
		third <- activeRunAcquisition{release: release, admitted: ok}
	}()
	assertActiveRunAcquireBlocked(t, third)
	first()
	first()
	acquired := waitActiveRunAcquire(t, third)
	if !acquired.admitted {
		t.Fatal("third run was not admitted after release")
	}
	if got := activeRunTotal(admission); got != 2 {
		t.Fatalf("active runs = %d, want 2", got)
	}
	second()
	acquired.release()
	if got := activeRunTotal(admission); got != 0 {
		t.Fatalf("active runs after release = %d, want 0", got)
	}
}

func TestActiveRunAdmissionResizeGrandfathersActiveRuns(t *testing.T) {
	admission := NewActiveRunAdmission(3)
	releases := make([]func(), 3)
	for index := range releases {
		var admitted bool
		releases[index], admitted = admission.acquire(t.Context())
		if !admitted {
			t.Fatalf("run %d was not admitted", index)
		}
	}
	admission.Resize(0)
	admission.Resize(3)
	admission.Resize(1)
	waiting := make(chan activeRunAcquisition, 1)
	go func() {
		release, ok := admission.acquire(t.Context())
		waiting <- activeRunAcquisition{release: release, admitted: ok}
	}()
	releases[0]()
	releases[1]()
	assertActiveRunAcquireBlocked(t, waiting)
	admission.Resize(2)
	acquired := waitActiveRunAcquire(t, waiting)
	if !acquired.admitted {
		t.Fatal("waiting run was not admitted after expansion")
	}
	if got := activeRunTotal(admission); got != 2 {
		t.Fatalf("active runs after expansion = %d, want 2", got)
	}
	releases[2]()
	acquired.release()
}

func TestActiveRunAdmissionHonorsCancellation(t *testing.T) {
	admission := NewActiveRunAdmission(0)
	occupied, admitted := admission.acquire(t.Context())
	if !admitted {
		t.Fatal("normalized admission did not admit one run")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if release, ok := admission.acquire(cancelled); ok || release != nil {
		t.Fatal("pre-cancelled run was admitted")
	}
	waiting, cancelWaiting := context.WithCancel(t.Context())
	result := make(chan activeRunAcquisition, 1)
	go func() {
		release, ok := admission.acquire(waiting)
		result <- activeRunAcquisition{release: release, admitted: ok}
	}()
	assertActiveRunAcquireBlocked(t, result)
	cancelWaiting()
	acquired := waitActiveRunAcquire(t, result)
	if acquired.admitted || acquired.release != nil {
		t.Fatal("cancelled waiting run was admitted")
	}
	occupied()
}

func assertActiveRunAcquireBlocked(
	t *testing.T,
	result <-chan activeRunAcquisition,
) {
	t.Helper()
	select {
	case acquired := <-result:
		t.Fatalf("run acquired capacity early: %+v", acquired)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitActiveRunAcquire(
	t *testing.T,
	result <-chan activeRunAcquisition,
) activeRunAcquisition {
	t.Helper()
	select {
	case acquired := <-result:
		return acquired
	case <-time.After(time.Second):
		t.Fatal("run admission did not resolve")

		return activeRunAcquisition{}
	}
}

func activeRunTotal(admission *ActiveRunAdmission) int {
	admission.mu.Lock()
	defer admission.mu.Unlock()

	return admission.active
}
