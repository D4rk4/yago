package contentcluster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type EvidenceReplacement struct {
	Previous      Assignment
	PreviousFound bool
	Current       Assignment
}

func (i *Index) ReplaceBatch(
	ctx context.Context,
	evidence []Evidence,
) ([]EvidenceReplacement, error) {
	prepared := make([]preparedEvidence, len(evidence))
	for position, item := range evidence {
		var err error
		prepared[position], err = prepareEvidence(ctx, i.limits, item)
		if err != nil {
			return nil, fmt.Errorf("prepare content cluster evidence %d: %w", position, err)
		}
	}

	replacements := make([]EvidenceReplacement, 0, len(prepared))
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		replacements = replacements[:0]
		for position, item := range prepared {
			replacement, err := i.replaceEvidence(tx, ctx, item)
			if err != nil {
				return fmt.Errorf("replace content cluster evidence %d: %w", position, err)
			}
			replacements = append(replacements, replacement)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("replace content cluster evidence batch: %w", err)
	}

	return replacements, nil
}

func (i *Index) replaceEvidence(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
) (EvidenceReplacement, error) {
	replacement := EvidenceReplacement{}
	previous, found, err := i.fingerprints.Get(tx, vault.Key(prepared.URL))
	if err != nil {
		return EvidenceReplacement{}, fmt.Errorf("read replaced content fingerprint: %w", err)
	}
	if found {
		replacement.Previous, err = i.existingAssignment(tx, previous.ClusterID)
		if err != nil {
			return EvidenceReplacement{}, err
		}
		replacement.PreviousFound = true
	}
	replacement.Current, err = i.replace(tx, ctx, prepared)
	if err != nil {
		return EvidenceReplacement{}, err
	}

	return replacement, nil
}
