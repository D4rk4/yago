package memvault

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (e *engine) BucketProvisioned(ctx context.Context, name vault.Name) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("context: %w", err)
	}
	e.mu.RLock()
	_, present := e.buckets[name]
	e.mu.RUnlock()

	return present, nil
}
