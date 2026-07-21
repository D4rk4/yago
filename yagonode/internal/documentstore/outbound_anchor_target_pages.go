package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) replaceOutboundAnchorDocumentTargets(
	ctx context.Context,
	replacement outboundAnchorDocumentReplacement,
	visit func([]Document) error,
) error {
	for first := 0; first < len(replacement.targets); first += outboundAnchorTargetPageSize {
		last := min(first+outboundAnchorTargetPageSize, len(replacement.targets))
		if err := d.replaceOutboundAnchorDocumentTargetPage(
			ctx,
			replacement,
			replacement.targets[first:last],
			visit,
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
	visit func([]Document) error,
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
	releaseWrite()
	if visit == nil {
		return nil
	}
	documents := make([]Document, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.visitDocument {
			documents = append(documents, normalizedDocument(mutation.document))
		}
	}
	if len(documents) == 0 {
		return nil
	}
	if err := visit(documents); err != nil {
		return fmt.Errorf("visit outbound anchor documents: %w", err)
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
		if mutation.encodedBytes < 0 || mutation.encodedBytes > maximumEncodedBytes {
			return nil, fmt.Errorf("outbound anchor target mutation byte limit exceeded")
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
