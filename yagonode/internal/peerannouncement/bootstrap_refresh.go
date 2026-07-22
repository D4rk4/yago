package peerannouncement

import (
	"context"
	"sync"
	"time"
)

const bootstrapRetryCooldown = 5 * time.Minute

type bootstrapRefresh struct {
	mu          sync.Mutex
	now         func() time.Time
	cooldown    time.Duration
	lastAttempt time.Time
	running     chan struct{}
}

func (r *bootstrapRefresh) begin(force bool) (chan struct{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running != nil {
		return r.running, false
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	cooldown := r.cooldown
	if cooldown <= 0 {
		cooldown = bootstrapRetryCooldown
	}
	if !force && !r.lastAttempt.IsZero() &&
		(now.Before(r.lastAttempt) || now.Sub(r.lastAttempt) < cooldown) {
		return nil, false
	}
	r.lastAttempt = now
	r.running = make(chan struct{})

	return r.running, true
}

func (r *bootstrapRefresh) finish(running chan struct{}) {
	r.mu.Lock()
	if r.running == running {
		r.running = nil
		close(running)
	}
	r.mu.Unlock()
}

func waitForBootstrapRefresh(ctx context.Context, running <-chan struct{}) {
	if running == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-running:
	}
}
