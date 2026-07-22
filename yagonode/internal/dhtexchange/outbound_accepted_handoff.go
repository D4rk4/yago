package dhtexchange

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

func (d OutboundDistributor) finishAcceptedHandoff(
	ctx context.Context,
	receipt DistributionReceipt,
	chunk OutboundChunk,
	handoff indextransfer.HandoffReceipt,
) (DistributionReceipt, error) {
	receipt.State = DistributionSent
	accepted, rejected := splitHandoffPostings(chunk.Postings, handoff.RejectedPostings)
	var outcomeErr error
	if len(rejected) != 0 {
		d.queue.cancelPostingCopies(rejected)
		restored, err := d.restoreRejectedPostings(ctx, rejected)
		receipt.RestoredPostings = restored
		if err != nil {
			outcomeErr = fmt.Errorf("restore rejected dht postings: %w", err)
		}
	}
	confirmable := d.queue.confirmablePostings(accepted)
	if d.confirmer != nil && len(confirmable) != 0 {
		confirmed, err := d.confirmAcceptedPostings(ctx, confirmable)
		receipt.ConfirmedPostings = confirmed
		if err != nil {
			outcomeErr = errors.Join(
				outcomeErr,
				fmt.Errorf("confirm sent dht chunk: %w", err),
			)
		}
	}

	return receipt, outcomeErr
}
