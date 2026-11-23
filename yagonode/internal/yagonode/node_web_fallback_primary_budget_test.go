package yagonode

import (
	"testing"
	"time"
)

func TestWebFallbackPrimaryStageLeavesWorkerHeadroom(t *testing.T) {
	if webFallbackPrimaryStageBudget != 3200*time.Millisecond {
		t.Fatalf("primary budget = %s", webFallbackPrimaryStageBudget)
	}
	stages := webFallbackPrimaryStageBudget + localExactRecoveryBudget
	workerBudget := interactiveSearchBudget - interactiveSearchCancellationGrace
	if headroom := workerBudget - stages; headroom < 150*time.Millisecond {
		t.Fatalf("primary stages = %s, headroom = %s", stages, headroom)
	}
}
