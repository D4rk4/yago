package yagonode

import (
	"context"
	"errors"
	"fmt"
)

const interactiveSearchCapacityFailure = "local search capacity exhausted"

var errInteractiveSearchCapacity = errors.New(interactiveSearchCapacityFailure)

type interactiveSearchAdmission struct {
	slots chan struct{}
}

func newInteractiveSearchAdmission(capacity int) *interactiveSearchAdmission {
	return &interactiveSearchAdmission{slots: make(chan struct{}, capacity)}
}

func (a *interactiveSearchAdmission) tryAcquire(ctx context.Context) (func(), error) {
	if err := context.Cause(ctx); err != nil {
		return nil, fmt.Errorf("interactive search admission: %w", err)
	}

	select {
	case a.slots <- struct{}{}:
		return func() { <-a.slots }, nil
	default:
		return nil, errInteractiveSearchCapacity
	}
}
