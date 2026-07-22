package peernews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) inspectKnownNews(
	ctx context.Context,
	id string,
	now time.Time,
) (bool, bool, error) {
	current := false
	expired := false
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		key := vault.Key(id)
		exists, err := p.storedKnownMarkerPresent(tx, key)
		if err != nil {
			return fmt.Errorf("check known news: %w", err)
		}
		if !exists {
			return nil
		}
		category, _, err := p.knownNewsCategory(tx, key)
		if err != nil {
			if !errors.Is(err, vault.ErrCorruptValue) {
				return fmt.Errorf("check known news category: %w", err)
			}
			category = knownMarker
		}
		created, err := newsIDCreation(id)
		if err != nil {
			return fmt.Errorf("check known news creation: %w", err)
		}
		current = newsCreationAdmitted(created, now, category)
		expired = !current

		return nil
	})
	if err != nil {
		return false, false, fmt.Errorf("inspect known news: %w", err)
	}

	return current, expired, nil
}
