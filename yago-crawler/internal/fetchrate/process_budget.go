package fetchrate

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type ProcessBudget struct {
	mu             sync.Mutex
	pagesPerSecond uint32
	nextAdmission  time.Time
	changed        chan struct{}
	waiting        atomic.Int64
}

func NewProcessBudget(pagesPerSecond uint32) *ProcessBudget {
	budget := &ProcessBudget{changed: make(chan struct{})}
	budget.Set(pagesPerSecond)

	return budget
}

func (b *ProcessBudget) Set(pagesPerSecond uint32) {
	if pagesPerSecond > yagocrawlcontract.MaximumProcessPagesPerSecond {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pagesPerSecond == pagesPerSecond {
		return
	}
	b.pagesPerSecond = pagesPerSecond
	b.nextAdmission = time.Time{}
	close(b.changed)
	b.changed = make(chan struct{})
}

func (b *ProcessBudget) PagesPerSecond() uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.pagesPerSecond
}

func (b *ProcessBudget) Wait(ctx context.Context) error {
	b.waiting.Add(1)
	defer b.waiting.Add(-1)
	for {
		b.mu.Lock()
		pagesPerSecond := b.pagesPerSecond
		if pagesPerSecond == 0 {
			b.mu.Unlock()

			return nil
		}
		now := time.Now()
		if !b.nextAdmission.After(now) {
			b.nextAdmission = now.Add(time.Second / time.Duration(pagesPerSecond))
			b.mu.Unlock()

			return nil
		}
		wait := b.nextAdmission.Sub(now)
		changed := b.changed
		b.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()

			return fmt.Errorf("wait for process fetch budget: %w", ctx.Err())
		case <-changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (b *ProcessBudget) Waiting() int {
	return int(b.waiting.Load())
}
