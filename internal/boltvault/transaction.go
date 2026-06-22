package boltvault

import (
	"context"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

type Txn struct {
	tx       *bolt.Tx
	writable bool
}

func (v *Vault) Update(ctx context.Context, fn func(*Txn) error) error {
	if v == nil || v.db == nil {
		return errVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if err := v.rejectAtCapacity(); err != nil {
		return err
	}

	if err := v.db.Update(func(tx *bolt.Tx) error {
		return fn(&Txn{tx: tx, writable: true})
	}); err != nil {
		return wrapTxnError("write storage", err)
	}

	return nil
}

func (v *Vault) View(ctx context.Context, fn func(*Txn) error) error {
	if v == nil || v.db == nil {
		return errVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	if err := v.db.View(func(tx *bolt.Tx) error {
		return fn(&Txn{tx: tx, writable: false})
	}); err != nil {
		return wrapTxnError("read storage", err)
	}

	return nil
}

func wrapTxnError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errReadOnly) {
		return err
	}
	if storageAtCapacityError(err) {
		return fmt.Errorf("%s: %w", operation, ErrAtCapacity)
	}

	return fmt.Errorf("%s: %w", operation, err)
}
