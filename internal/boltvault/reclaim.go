package boltvault

import (
	"context"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

func (v *Vault) Reclaim(ctx context.Context, fn func(*Txn) error) error {
	if v == nil || v.db == nil {
		return errVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	if err := v.db.Update(func(tx *bolt.Tx) error {
		return fn(&Txn{tx: tx, writable: true})
	}); err != nil {
		return wrapTxnError("reclaim storage", err)
	}

	return nil
}
