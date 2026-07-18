package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInteractiveSearchAdmissionWaitStopsAtDeadline(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.acquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	waitingRelease, err := admission.acquire(ctx)
	if waitingRelease != nil || !errors.Is(err, context.DeadlineExceeded) ||
		len(admission.slots) != 1 {
		t.Fatalf(
			"waiting acquire = %v, release = %t, occupied = %d",
			err,
			waitingRelease != nil,
			len(admission.slots),
		)
	}
}
