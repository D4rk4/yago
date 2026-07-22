package peerroster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (r *roster) isSelf(peer yagomodel.Hash) bool {
	return peer == r.self
}

func (r *roster) removeSelf(ctx context.Context) error {
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := r.deleteRosterEntry(tx, r.key(r.self)); err != nil {
			return fmt.Errorf("delete self peer: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("remove self peer: %w", err)
	}

	return nil
}
