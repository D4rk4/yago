package crawllease

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

var ErrLeaseLost = errors.New("crawl lease lost")

type grant struct {
	ctx       context.Context
	cancel    context.CancelCauseFunc
	expiresAt time.Time
	renewedAt time.Time
	confirmed bool
}

type GrantRegistry struct {
	mu                  sync.Mutex
	grants              map[string]*grant
	capacity            int
	now                 func() time.Time
	wake                chan struct{}
	availabilityChanges chan struct{}
	leaseLosses         chan struct{}
}

func NewGrantRegistry(ctx context.Context, capacity int) *GrantRegistry {
	return newGrantRegistry(ctx, capacity, time.Now, true)
}

func newGrantRegistry(
	ctx context.Context,
	capacity int,
	now func() time.Time,
	watch bool,
) *GrantRegistry {
	registry := &GrantRegistry{
		grants:              make(map[string]*grant, capacity),
		capacity:            max(1, capacity),
		now:                 now,
		wake:                make(chan struct{}, 1),
		availabilityChanges: make(chan struct{}),
		leaseLosses:         make(chan struct{}),
	}
	if watch {
		go registry.watch(ctx)
	}

	return registry
}

func (r *GrantRegistry) Track(leaseID string) error {
	_, err := r.TrackMany([]string{leaseID})

	return err
}

func (r *GrantRegistry) ActiveLeaseIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireAndSignalLocked(r.now())
	active := make([]string, 0, len(r.grants))
	for leaseID := range r.grants {
		active = append(active, leaseID)
	}
	slices.Sort(active)

	return active
}

func (r *GrantRegistry) Renew(
	requestStarted time.Time,
	timeToLive time.Duration,
	requested []string,
	renewed []string,
) {
	requestedSet := make(map[string]struct{}, len(requested))
	for _, leaseID := range requested {
		requestedSet[leaseID] = struct{}{}
	}
	renewedSet := make(map[string]struct{}, len(renewed))
	for _, leaseID := range renewed {
		renewedSet[leaseID] = struct{}{}
	}
	deadline := requestStarted.Add(timeToLive)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	availabilityChanged := false
	leaseLost := false
	for leaseID := range requestedSet {
		current, exists := r.grants[leaseID]
		if !exists {
			continue
		}
		if current.confirmed && requestStarted.Before(current.renewedAt) {
			continue
		}
		_, accepted := renewedSet[leaseID]
		if !accepted || timeToLive <= 0 || !deadline.After(now) ||
			current.confirmed && !requestStarted.Before(current.expiresAt) {
			r.loseLocked(leaseID, current)
			availabilityChanged = true
			leaseLost = true

			continue
		}
		if !current.confirmed {
			availabilityChanged = true
		}
		current.confirmed = true
		current.renewedAt = requestStarted
		if deadline.After(current.expiresAt) {
			current.expiresAt = deadline
		}
	}
	expired := r.expireLocked(now)
	availabilityChanged = expired || availabilityChanged
	r.signalLocked()
	if availabilityChanged {
		r.signalAvailabilityChangeLocked()
	}
	if leaseLost || expired {
		r.signalLeaseLossLocked()
	}
}

func (r *GrantRegistry) Confirmed(leaseID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireAndSignalLocked(r.now())
	current, exists := r.grants[leaseID]

	return exists && current.confirmed
}

func (r *GrantRegistry) Context(leaseID string) (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireAndSignalLocked(r.now())
	current, exists := r.grants[leaseID]
	if !exists || !current.confirmed {
		return nil, false
	}

	return current.ctx, true
}

func (r *GrantRegistry) Revoke(leaseID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, exists := r.grants[leaseID]; exists {
		r.loseLocked(leaseID, current)
		r.signalLocked()
		r.signalAvailabilityChangeLocked()
	}
}

func (r *GrantRegistry) AvailabilityChanges() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireAndSignalLocked(r.now())

	return r.availabilityChanges
}

func (r *GrantRegistry) watch(ctx context.Context) {
	for {
		wait := r.nextExpiry()
		if wait < 0 {
			select {
			case <-r.wake:
			case <-ctx.Done():
				r.close()

				return
			}

			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			r.expire()
		case <-r.wake:
			timer.Stop()
		case <-ctx.Done():
			timer.Stop()
			r.close()

			return
		}
	}
}

func (r *GrantRegistry) nextExpiry() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.expireAndSignalLocked(now)
	var earliest time.Time
	for _, current := range r.grants {
		if !current.confirmed {
			continue
		}
		if earliest.IsZero() || current.expiresAt.Before(earliest) {
			earliest = current.expiresAt
		}
	}
	if earliest.IsZero() {
		return -1
	}

	return max(0, earliest.Sub(now))
}

func (r *GrantRegistry) expire() {
	r.mu.Lock()
	r.expireAndSignalLocked(r.now())
	r.mu.Unlock()
}

func (r *GrantRegistry) expireAndSignalLocked(now time.Time) {
	if r.expireLocked(now) {
		r.signalAvailabilityChangeLocked()
		r.signalLeaseLossLocked()
	}
}

func (r *GrantRegistry) expireLocked(now time.Time) bool {
	changed := false
	for leaseID, current := range r.grants {
		if current.confirmed && !current.expiresAt.After(now) {
			r.loseLocked(leaseID, current)
			changed = true
		}
	}

	return changed
}

func (r *GrantRegistry) loseLocked(leaseID string, current *grant) {
	current.cancel(ErrLeaseLost)
	delete(r.grants, leaseID)
}

func (r *GrantRegistry) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := len(r.grants) != 0
	for leaseID, current := range r.grants {
		r.loseLocked(leaseID, current)
	}
	if changed {
		r.signalAvailabilityChangeLocked()
	}
}

func (r *GrantRegistry) signalLocked() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *GrantRegistry) signalAvailabilityChangeLocked() {
	close(r.availabilityChanges)
	r.availabilityChanges = make(chan struct{})
}

func (r *GrantRegistry) signalLeaseLossLocked() {
	close(r.leaseLosses)
	r.leaseLosses = make(chan struct{})
}
