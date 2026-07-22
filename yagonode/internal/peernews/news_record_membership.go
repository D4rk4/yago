package peernews

import (
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) knownRecordMatches(tx *vault.Txn, record Record) (bool, error) {
	key := vault.Key(record.ID())
	found, err := p.storedKnownMarkerPresent(tx, key)
	if err != nil {
		return false, fmt.Errorf("read known news membership: %w", err)
	}
	if !found {
		return false, nil
	}
	category, exact, err := p.knownNewsCategory(tx, key)
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return false, fmt.Errorf("read known news category: %w", err)
		}
		category = knownMarker
		exact = false
	}
	if !exact {
		return record.Category == "", nil
	}

	return category == record.Category, nil
}

func (p *Pool) storedKnownMarkerPresent(tx *vault.Txn, key vault.Key) (bool, error) {
	size, found, err := p.known.EncodedSize(tx, key)
	if err != nil {
		return false, fmt.Errorf("inspect known news marker: %w", err)
	}
	if !found {
		return false, nil
	}
	if size != len(knownMarker) {
		return false, fmt.Errorf(
			"%w: %w: known news marker size %d",
			vault.ErrCorruptValue,
			ErrBadNewsRecord,
			size,
		)
	}
	_, present, err := p.known.Get(tx, key)
	if err != nil {
		return false, fmt.Errorf("read known news marker: %w", err)
	}
	if !present {
		return false, fmt.Errorf(
			"%w: known news marker disappeared during read",
			vault.ErrCorruptValue,
		)
	}

	return true, nil
}
