package worker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestFetchAdmissionBoundsConcurrentPages(t *testing.T) {
	admission := newFetchAdmission(2)
	releaseFirst, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("first page was not admitted")
	}
	releaseSecond, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("second page was not admitted")
	}
	if release, admitted := admission.TryAcquire(); admitted {
		release()
		t.Fatal("page above the configured concurrency was admitted")
	}
	releaseFirst()
	releaseThird, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("page was not admitted after a worker slot was released")
	}
	releaseSecond()
	releaseThird()
}

func TestFetchAdmissionChangesOnlyForValidLimits(t *testing.T) {
	admission := newFetchAdmission(1)
	if !admission.SetMaximum(2) {
		t.Fatal("valid page concurrency was rejected")
	}
	if admission.SetMaximum(0) ||
		admission.SetMaximum(yagocrawlcontract.MaximumFetchWorkerConcurrency+1) {
		t.Fatal("invalid page concurrency was accepted")
	}
	releaseFirst, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("first page was not admitted")
	}
	releaseSecond, admitted := admission.TryAcquire()
	if !admitted {
		t.Fatal("invalid update changed the accepted concurrency")
	}
	if release, admitted := admission.TryAcquire(); admitted {
		release()
		t.Fatal("page above the unchanged concurrency was admitted")
	}
	releaseFirst()
	releaseSecond()
}
