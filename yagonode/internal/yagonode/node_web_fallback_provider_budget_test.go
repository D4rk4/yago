package yagonode

import (
	"testing"
	"time"
)

func TestWebFallbackProviderStageBudgetFollowsStartMode(t *testing.T) {
	tests := []struct {
		name   string
		config webFallbackConfig
		want   time.Duration
	}{
		{
			name:   "explicit",
			config: webFallbackConfig{Privacy: webFallbackPrivacyExplicit},
			want:   900 * time.Millisecond,
		},
		{
			name:   "miss",
			config: webFallbackConfig{Privacy: webFallbackPrivacyEnabled},
			want:   900 * time.Millisecond,
		},
		{
			name:   "always",
			config: webFallbackConfig{Privacy: webFallbackPrivacyAlways},
			want:   1500 * time.Millisecond,
		},
		{
			name: "legacy parallel",
			config: webFallbackConfig{
				Privacy: webFallbackPrivacyEnabled,
				Trigger: webFallbackTriggerParallel,
			},
			want: 1500 * time.Millisecond,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := webFallbackProviderStageBudget(test.config); got != test.want {
				t.Fatalf("provider stage budget = %v, want %v", got, test.want)
			}
		})
	}
}
