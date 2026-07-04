package vault

import (
	"context"
	"errors"
	"fmt"
)

type Txn struct {
	etx EngineTxn
}

func (v *Vault) Update(ctx context.Context, fn func(*Txn) error) error {
	if v == nil || v.engine == nil {
		return errVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	if err := v.engine.Update(ctx, func(etx EngineTxn) error {
		return fn(&Txn{etx: etx})
	}); err != nil {
		return wrapTxnError("write storage", err)
	}

	return nil
}

func (v *Vault) View(ctx context.Context, fn func(*Txn) error) error {
	if v == nil || v.engine == nil {
		return errVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	if err := v.engine.View(ctx, func(etx EngineTxn) error {
		return fn(&Txn{etx: etx})
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
	if errors.Is(err, ErrAtCapacity) {
		return err
	}

	return fmt.Errorf("%s: %w", operation, err)
}
