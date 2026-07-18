package yagonode

import (
	"context"
	"fmt"
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
