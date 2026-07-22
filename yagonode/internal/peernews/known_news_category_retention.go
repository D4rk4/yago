package peernews

import (
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) pruneKnownNewsCategoryRecord(tx *vault.Txn, candidate vault.Key) error {
	_, categoryFound, categoryErr := p.storedKnownCategoryEvidence(tx, candidate)
	if categoryErr != nil && !errors.Is(categoryErr, vault.ErrCorruptValue) {
		return fmt.Errorf("read known news category: %w", categoryErr)
	}
	markerFound, markerErr := p.storedKnownMarkerPresent(tx, candidate)
	if markerErr != nil && !errors.Is(markerErr, vault.ErrCorruptValue) {
		return fmt.Errorf("read known news marker: %w", markerErr)
	}
	if categoryErr == nil && categoryFound && markerErr == nil && markerFound {
		return nil
	}
	if _, err := p.knownCategories.Delete(tx, candidate); err != nil {
		return fmt.Errorf("evict invalid known news category: %w", err)
	}

	return nil
}
