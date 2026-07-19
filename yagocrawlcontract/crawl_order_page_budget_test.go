package yagocrawlcontract_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAutomaticDiscoveryOrderBoundsLegacyWholeRunBudget(t *testing.T) {
	t.Parallel()

	perHost := 250
	for name, maximum := range map[string]*int{
		"missing":   nil,
		"unlimited": pointerTo(0),
		"larger":    pointerTo(50_000),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			order := yagocrawlcontract.CrawlOrder{
				Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
				Profile: yagocrawlcontract.CrawlProfile{
					MaxPagesPerHost: perHost,
					MaxPagesPerRun:  maximum,
				},
			}
			if got := order.EffectiveMaxPagesPerRun(50_000); got != perHost {
				t.Fatalf("effective maximum = %d, want %d", got, perHost)
			}
		})
	}
}

func TestQueuedAutomaticDiscoveryOrderWithoutWholeRunBudgetUsesHostLimit(t *testing.T) {
	t.Parallel()

	order, err := yagocrawlcontract.UnmarshalCrawlOrder([]byte(
		`{"Priority":"automatic_discovery","Profile":{"MaxPagesPerHost":250}}`,
	))
	if err != nil {
		t.Fatalf("decode queued automatic order: %v", err)
	}
	if got := order.EffectiveMaxPagesPerRun(50_000); got != 250 {
		t.Fatalf("queued automatic maximum = %d, want 250", got)
	}
}

func TestAutomaticDiscoveryOrderKeepsStricterWholeRunBudget(t *testing.T) {
	t.Parallel()

	maximum := 100
	order := yagocrawlcontract.CrawlOrder{
		Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		Profile: yagocrawlcontract.CrawlProfile{
			MaxPagesPerHost: 250,
			MaxPagesPerRun:  &maximum,
		},
	}
	if got := order.EffectiveMaxPagesPerRun(50_000); got != maximum {
		t.Fatalf("effective maximum = %d, want %d", got, maximum)
	}
}

func TestManualOrderDoesNotDeriveWholeRunBudgetFromHostBudget(t *testing.T) {
	t.Parallel()

	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.CrawlProfile{MaxPagesPerHost: 250},
	}
	if got := order.EffectiveMaxPagesPerRun(50_000); got != 50_000 {
		t.Fatalf("effective maximum = %d, want 50000", got)
	}
}

func pointerTo(value int) *int {
	return &value
}
