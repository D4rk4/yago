package peernews

import (
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) queuedNewsEvidenceCandidate(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
) (string, queuedNewsEvidence, bool, error) {
	wire, _, _, err := p.readQueuedNewsWire(tx, key)
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return "", queuedNewsEvidence{}, false, nil
		}

		return "", queuedNewsEvidence{}, false, fmt.Errorf(
			"read queued news evidence: %w", err,
		)
	}
	record, decoded := decodeQueuedNewsRecord(wire)
	if !decoded || !newsCreationAdmitted(record.Created, now, record.Category) {
		return "", queuedNewsEvidence{}, false, nil
	}
	priority, selected, err := p.queuedNewsEvidencePriority(tx, record)
	if err != nil || !selected {
		return "", queuedNewsEvidence{}, false, err
	}

	return record.ID(), queuedNewsEvidence{
		category: record.Category, generation: knownCategoryGeneration(record),
		key: append(vault.Key(nil), key...), priority: priority,
	}, true, nil
}

func (p *Pool) queuedNewsEvidencePriority(
	tx *vault.Txn,
	record Record,
) (int, bool, error) {
	identity := vault.Key(record.ID())
	known, err := p.storedKnownMarkerPresent(tx, identity)
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return 0, false, nil
		}

		return 0, false, fmt.Errorf("read queued news evidence membership: %w", err)
	}
	if !known {
		return 0, false, nil
	}
	encoded, exact, categoryErr := p.storedKnownCategoryEvidence(tx, identity)
	if categoryErr != nil {
		if errors.Is(categoryErr, vault.ErrCorruptValue) {
			return 0, true, nil
		}

		return 0, false, fmt.Errorf("read queued news evidence category: %w", categoryErr)
	}
	if !exact {
		return 0, true, nil
	}
	category, generation, bound, _ := decodeKnownCategoryEvidence(encoded)
	if category != record.Category {
		return 0, true, nil
	}
	if !bound {
		return 1, true, nil
	}
	if generation == knownCategoryGeneration(record) {
		return 2, true, nil
	}

	return 0, true, nil
}
