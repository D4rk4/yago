package dhtexchange

import (
	"context"
	"errors"
	"fmt"
)

func (d OutboundDistributor) finishRejectedHandoff(
	ctx context.Context,
	receipt DistributionReceipt,
	chunk OutboundChunk,
	handoffErr error,
) (DistributionReceipt, error) {
	receipt.State = DistributionHandoffRejected
	d.queue.cancelPostingCopies(chunk.Postings)
	restored, restoreErr := d.restoreRejectedPostings(ctx, chunk.Postings)
	receipt.RestoredPostings = restored
	if restoreErr != nil {
		restoreFailure := fmt.Errorf("restore rejected dht chunk: %w", restoreErr)
		if handoffErr == nil {
			return receipt, restoreFailure
		}

		return receipt, errors.Join(
			fmt.Errorf("send dht chunk: %w", handoffErr),
			restoreFailure,
		)
	}
	if handoffErr != nil {
		return receipt, fmt.Errorf("send dht chunk: %w", handoffErr)
	}

	return receipt, nil
}
