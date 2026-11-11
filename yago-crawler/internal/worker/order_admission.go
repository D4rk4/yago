package worker

import (
	"context"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type orderAdmission struct {
	mutex             sync.Mutex
	changed           chan struct{}
	active            int
	maximumActiveRuns int
}

func newOrderAdmission() *orderAdmission {
	return &orderAdmission{
		changed:           make(chan struct{}),
		maximumActiveRuns: yagocrawlcontract.DefaultActiveCrawlRunConcurrency,
	}
}

func (admission *orderAdmission) Acquire(ctx context.Context) error {
	for {
		admission.mutex.Lock()
		if admission.active < admission.maximumActiveRuns {
			admission.active++
			admission.mutex.Unlock()
			return nil
		}
		changed := admission.changed
		admission.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (admission *orderAdmission) Release() {
	admission.mutex.Lock()
	if admission.active > 0 {
		admission.active--
	}
	admission.signalLocked()
	admission.mutex.Unlock()
}

func (admission *orderAdmission) SetMaximumActiveRuns(maximum uint32) bool {
	if maximum < 1 || maximum > yagocrawlcontract.MaximumActiveCrawlRunConcurrency {
		return false
	}
	admission.mutex.Lock()
	admission.maximumActiveRuns = int(maximum)
	admission.signalLocked()
	admission.mutex.Unlock()
	return true
}

func (admission *orderAdmission) signalLocked() {
	close(admission.changed)
	admission.changed = make(chan struct{})
}
