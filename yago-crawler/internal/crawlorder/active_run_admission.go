package crawlorder

import (
	"context"
	"sync"
)

type ActiveRunAdmission struct {
	mu      sync.Mutex
	maximum int
	active  int
	changed chan struct{}
}

func NewActiveRunAdmission(maximum int) *ActiveRunAdmission {
	return &ActiveRunAdmission{
		maximum: max(1, maximum),
		changed: make(chan struct{}),
	}
}

func (a *ActiveRunAdmission) Resize(maximum int) {
	if maximum < 1 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.maximum == maximum {
		return
	}
	a.maximum = maximum
	a.signalLocked()
}

func (a *ActiveRunAdmission) acquire(ctx context.Context) (func(), bool) {
	for {
		if ctx.Err() != nil {
			return nil, false
		}
		a.mu.Lock()
		if a.active < a.maximum {
			a.active++
			a.mu.Unlock()
			var once sync.Once

			return func() { once.Do(a.release) }, true
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, false
		}
	}
}

func (a *ActiveRunAdmission) release() {
	a.mu.Lock()
	a.active--
	a.signalLocked()
	a.mu.Unlock()
}

func (a *ActiveRunAdmission) signalLocked() {
	close(a.changed)
	a.changed = make(chan struct{})
}
