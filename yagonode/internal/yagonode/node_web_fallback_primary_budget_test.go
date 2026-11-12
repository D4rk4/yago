package yagonode

import (
	"testing"
	"time"
)

func TestWebFallbackPrimaryStageLeavesWorkerHeadroom(t *testing.T) {
	if webFallbackExactStageBudget != 600*time.Millisecond {
		t.Fatalf("supplement primary budget = %s", webFallbackExactStageBudget)
	}
	if webFallbackParallelExactStageBudget != 1400*time.Millisecond {
		t.Fatalf("parallel primary budget = %s", webFallbackParallelExactStageBudget)
	}
	stages := webFallbackParallelExactStageBudget + localExactRecoveryBudget
	workerBudget := interactiveSearchBudget - interactiveSearchCancellationGrace
	if headroom := workerBudget - stages; headroom < 150*time.Millisecond {
		t.Fatalf("primary stages = %s, headroom = %s", stages, headroom)
	}
	if budget := webFallbackPrimaryStageBudget(webFallbackConfig{
		Privacy: webFallbackPrivacyEnabled,
	}); budget != webFallbackExactStageBudget {
		t.Fatalf("supplement configured budget = %s, want %s", budget, webFallbackExactStageBudget)
	}
	if budget := webFallbackPrimaryStageBudget(webFallbackConfig{
		Privacy: webFallbackPrivacyAlways,
	}); budget != webFallbackParallelExactStageBudget {
		t.Fatalf(
			"always configured budget = %s, want %s",
			budget,
			webFallbackParallelExactStageBudget,
		)
	}
}
