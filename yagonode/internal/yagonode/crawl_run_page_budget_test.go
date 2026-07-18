package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlRunPageBudgetBoundaries(t *testing.T) {
	budget := newCrawlRunPageBudget(123)
	if got := budget.MaxPagesPerRun(); got != 123 {
		t.Fatalf("max pages per run = %d, want 123", got)
	}
	budget.Set(-1)
	if got := budget.MaxPagesPerRun(); got != 123 {
		t.Fatalf("max pages per run after invalid update = %d, want 123", got)
	}

	var absent *crawlRunPageBudget
	if got := absent.MaxPagesPerRun(); got != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("absent max pages per run = %d, want %d", got,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}
	var runtime *crawlRuntime
	if got := runtime.MaxPagesPerRun(); got != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("nil runtime max pages per run = %d, want %d", got,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}
}

func TestCrawlPageBudgetSource(t *testing.T) {
	fallback := crawlPageBudgetSource(nil)
	if got := fallback(); got != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("fallback max pages per run = %d, want %d", got,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}
	runtime := &crawlRuntime{pageBudget: newCrawlRunPageBudget(456)}
	current := crawlPageBudgetSource(runtime)
	if got := current(); got != 456 {
		t.Fatalf("current max pages per run = %d, want 456", got)
	}
}
