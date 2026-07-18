package boltvault

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (e *engine) BucketProvisioned(ctx context.Context, name vault.Name) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("context: %w", err)
	}
	present := false
	if err := e.db.View(func(tx *bolt.Tx) error {
		present = tx.Bucket([]byte(name)) != nil

		return nil
	}); err != nil {
		return false, fmt.Errorf("inspect bucket %s: %w", name, err)
	}

	return present, nil
}
