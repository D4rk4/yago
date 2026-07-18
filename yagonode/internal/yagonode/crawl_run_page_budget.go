package yagonode

import (
	"sync/atomic"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlRunPageBudget struct {
	maximum atomic.Int64
}

func newCrawlRunPageBudget(value int) *crawlRunPageBudget {
	budget := &crawlRunPageBudget{}
	budget.Set(value)

	return budget
}

func (b *crawlRunPageBudget) Set(value int) {
	if b != nil && value >= 0 {
		b.maximum.Store(int64(value))
	}
}

func (b *crawlRunPageBudget) MaxPagesPerRun() int {
	if b == nil {
		return yagocrawlcontract.DefaultMaxPagesPerRun
	}

	return int(b.maximum.Load())
}

func crawlPageBudgetSource(runtime crawlProcess) func() int {
	provider, ok := runtime.(interface{ MaxPagesPerRun() int })
	if !ok {
		return func() int { return yagocrawlcontract.DefaultMaxPagesPerRun }
	}

	return provider.MaxPagesPerRun
}
