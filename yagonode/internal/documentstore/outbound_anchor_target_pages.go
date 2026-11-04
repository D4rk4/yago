package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) replaceOutboundAnchorDocumentTargets(
	ctx context.Context,
	replacement outboundAnchorDocumentReplacement,
) error {
	for first := 0; first < len(replacement.targets); first += outboundAnchorTargetPageSize {
		last := min(first+outboundAnchorTargetPageSize, len(replacement.targets))
		if err := d.replaceOutboundAnchorDocumentTargetPage(
			ctx,
			replacement,
			replacement.targets[first:last],
		); err != nil {
			return err
		}
	}

	return nil
}

func (d documentVault) replaceOutboundAnchorDocumentTargetPage(
	ctx context.Context,
	replacement outboundAnchorDocumentReplacement,
	targetURLs []string,
) error {
	releaseWrite, err := d.enterStoredDocumentWrite(ctx)
	if err != nil {
		return err
	}
	defer releaseWrite()
	releaseTargets, err := d.urlBoundaries.lockWrites(ctx, targetURLs)
	if err != nil {
		return err
	}
	defer releaseTargets()
	mutations, err := d.prepareOutboundAnchorTargetMutations(
		ctx,
		replacement,
		targetURLs,
	)
	if err != nil {
		return err
	}

	return d.storeOutboundAnchorTargetMutations(ctx, mutations)
}

func (d documentVault) storeOutboundAnchorTargetMutations(
	ctx context.Context,
	mutations []outboundAnchorTargetMutation,
) error {
	batches, err := outboundAnchorTargetMutationBatches(
		mutations,
		outboundAnchorMutationMaximumRows,
		outboundAnchorMutationMaximumEncodedBytes,
	)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if err := d.storeOutboundAnchorTargetMutationBatch(ctx, batch); err != nil {
			return err
		}
	}

	return nil
}

func outboundAnchorTargetMutationBatches(
	mutations []outboundAnchorTargetMutation,
	maximumRows int,
	maximumEncodedBytes int,
) ([][]outboundAnchorTargetMutation, error) {
	if maximumRows < 1 || maximumEncodedBytes < 1 {
		return nil, fmt.Errorf("outbound anchor mutation budget must be positive")
	}
	batches := make([][]outboundAnchorTargetMutation, 0)
	batch := make([]outboundAnchorTargetMutation, 0, len(mutations))
	rows := 0
	encodedBytes := 0
	for _, mutation := range mutations {
		mutationRows := mutation.storageRows()
		if mutationRows == 0 {
			continue
		}
		if mutationRows > maximumRows {
			return nil, fmt.Errorf("outbound anchor target mutation row limit exceeded")
		}
		if mutation.encodedBytes < 0 {
			return nil, fmt.Errorf("outbound anchor target mutation size must not be negative")
		}
		if rows+mutationRows > maximumRows ||
			len(batch) > 0 && encodedBytes+mutation.encodedBytes > maximumEncodedBytes {
			batches = append(batches, batch)
			batch = make([]outboundAnchorTargetMutation, 0, len(mutations))
			rows = 0
			encodedBytes = 0
		}
		batch = append(batch, mutation)
		rows += mutationRows
		encodedBytes += mutation.encodedBytes
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}

	return batches, nil
}

func (d documentVault) storeOutboundAnchorTargetMutationBatch(
	ctx context.Context,
	mutations []outboundAnchorTargetMutation,
) error {
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, mutation := range mutations {
			if err := d.storeOutboundAnchorTargetMutation(tx, mutation); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("store outbound anchor target mutations: %w", err)
	}

	return nil
}
