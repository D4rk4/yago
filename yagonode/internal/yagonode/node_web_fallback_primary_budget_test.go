package yagonode

import (
	"testing"
	"time"
)

func TestWebFallbackPrimaryStageBudgetFollowsStartMode(t *testing.T) {
	if webFallbackParallelExactStageBudget != 1400*time.Millisecond {
		t.Fatalf("parallel primary budget = %s", webFallbackParallelExactStageBudget)
	}
	parallelStages := webFallbackParallelExactStageBudget + localExactRecoveryBudget
	workerBudget := interactiveSearchBudget - interactiveSearchCancellationGrace
	if headroom := workerBudget - parallelStages; headroom < 150*time.Millisecond {
		t.Fatalf("parallel primary stages = %s, headroom = %s", parallelStages, headroom)
	}

	for _, test := range []struct {
		name   string
		config webFallbackConfig
		want   time.Duration
	}{
		{
			name: "miss",
			config: webFallbackConfig{
				Privacy: webFallbackPrivacyEnabled,
			},
			want: webFallbackExactStageBudget,
		},
		{
			name: "always",
			config: webFallbackConfig{
				Privacy: webFallbackPrivacyAlways,
			},
			want: webFallbackParallelExactStageBudget,
		},
		{
			name: "legacy parallel",
			config: webFallbackConfig{
				Privacy: webFallbackPrivacyEnabled,
				Trigger: webFallbackTriggerParallel,
			},
			want: webFallbackParallelExactStageBudget,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := webFallbackPrimaryStageBudget(test.config)
			if got != test.want {
				t.Fatalf("budget = %s, want %s", got, test.want)
			}
		})
	}
}
