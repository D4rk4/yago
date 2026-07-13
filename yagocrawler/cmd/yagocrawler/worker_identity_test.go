package main

import (
	"strings"
	"testing"
)

func TestInstanceWorkerIDIsUniqueAndRetainsConfiguredPrefix(t *testing.T) {
	first := instanceWorkerID("crawler-a")
	second := instanceWorkerID("crawler-a")
	if first == second {
		t.Fatalf("worker identities must differ: %q", first)
	}
	for _, workerID := range []string{first, second} {
		if !strings.HasPrefix(workerID, "crawler-a-") {
			t.Fatalf("worker identity = %q, want configured prefix", workerID)
		}
	}
}

func TestInstanceWorkerIDUsesDefaultPrefix(t *testing.T) {
	workerID := instanceWorkerID(" ")
	if !strings.HasPrefix(workerID, DefaultWorkerID+"-") {
		t.Fatalf("worker identity = %q, want default prefix", workerID)
	}
}
