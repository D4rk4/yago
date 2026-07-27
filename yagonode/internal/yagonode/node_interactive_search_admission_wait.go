package yagonode

import (
	"context"
	"fmt"
	"time"
)

func (a *interactiveSearchAdmission) acquire(ctx context.Context) (func(), error) {
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("interactive search admission: %w", err)
	}

	select {
	case a.slots <- struct{}{}:
		return func() { <-a.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("interactive search admission: %w", context.Cause(ctx))
	}
}

// acquireWithin waits a bounded time for a slot. Exceeding the bound reports
// capacity exhaustion rather than a deadline, because the caller's own deadline
// has not passed -- the node simply had no room -- and the two need different
// words on the surface.
func (a *interactiveSearchAdmission) acquireWithin(
	ctx context.Context,
	wait time.Duration,
) (func(), error) {
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("interactive search admission: %w", err)
	}
	// A non-positive wait would arm an already-fired timer, and the select below
	// would then choose between a free slot and an expired deadline at random:
	// half the callers would be told the node is at capacity while a slot sat
	// open. A caller that forgets to state its wait gets the process default
	// rather than a coin flip.
	if wait <= 0 {
		wait = interactiveSearchAdmissionWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case a.slots <- struct{}{}:
		return func() { <-a.slots }, nil
	case <-timer.C:
		return nil, errInteractiveSearchCapacity
	case <-ctx.Done():
		return nil, fmt.Errorf("interactive search admission: %w", context.Cause(ctx))
	}
}
