package frontier_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

func TestEffectivePagesPerMinuteUsesDefaultAndExplicitRates(t *testing.T) {
	t.Parallel()

	f := frontier.NewFrontier(4, nil, frontier.WithDefaultRunRate(30))
	provenance := []byte("run")
	if got := f.EffectivePagesPerMinute(provenance); got != 30 {
		t.Fatalf("default pages per minute = %d, want 30", got)
	}

	f.SetRate(provenance, 45)
	if got := f.EffectivePagesPerMinute(provenance); got != 45 {
		t.Fatalf("explicit pages per minute = %d, want 45", got)
	}

	f.SetRate(provenance, 0)
	if got := f.EffectivePagesPerMinute(provenance); got != 0 {
		t.Fatalf("unlimited pages per minute = %d, want 0", got)
	}
	if got := f.EffectivePagesPerMinute([]byte("other")); got != 30 {
		t.Fatalf("other run pages per minute = %d, want default 30", got)
	}
}
