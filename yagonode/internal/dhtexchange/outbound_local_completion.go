package dhtexchange

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	DistributionRestorePending      DistributionState = "restore_pending"
	DistributionRestored            DistributionState = "restored"
	DistributionConfirmationPending DistributionState = "confirmation_pending"
	DistributionConfirmed           DistributionState = "confirmed"
)

func (d OutboundDistributor) retryLocalCompletion(
	ctx context.Context,
	gates GateReport,
) (DistributionReceipt, bool, error) {
	if postings := d.queue.pendingRestore(); len(postings) != 0 {
		receipt := DistributionReceipt{
			State:        DistributionRestorePending,
			Gates:        gates,
			PostingCount: len(postings),
		}
		restored, err := d.restoreLocally(ctx, postings)
		if err != nil {
			return receipt, true, fmt.Errorf("retry rejected dht restore: %w", err)
		}
		d.queue.completePendingRestore()
		receipt.State = DistributionRestored
		receipt.RestoredPostings = restored

		return receipt, true, nil
	}

	if postings := d.queue.pendingTransferConfirmation(); len(postings) != 0 {
		receipt := DistributionReceipt{
			State:        DistributionConfirmationPending,
			Gates:        gates,
			PostingCount: len(postings),
		}
		confirmed, err := d.confirmLocally(ctx, postings)
		if err != nil {
			return receipt, true, fmt.Errorf("retry sent dht confirmation: %w", err)
		}
		d.queue.completePendingTransferConfirmation()
		receipt.State = DistributionConfirmed
		receipt.ConfirmedPostings = confirmed

		return receipt, true, nil
	}

	return DistributionReceipt{}, false, nil
}

func (d OutboundDistributor) restoreRejectedPostings(
	ctx context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	restored, err := d.restoreLocally(ctx, postings)
	if err != nil {
		d.queue.retainPendingRestore(postings)
	}

	return restored, err
}

func (d OutboundDistributor) restoreLocally(
	ctx context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	restored, err := d.restorer.RestoreOutboundWords(ctx, outboundRestoreWords(postings))
	if err != nil {
		return 0, fmt.Errorf("restore outbound words: %w", err)
	}

	return restored, nil
}

func (d OutboundDistributor) confirmAcceptedPostings(
	ctx context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	confirmed, err := d.confirmLocally(ctx, postings)
	if err != nil {
		d.queue.retainPendingTransferConfirmation(postings)
	}

	return confirmed, err
}

func (d OutboundDistributor) confirmLocally(
	ctx context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	confirmed, err := d.confirmer.ConfirmTransferred(ctx, postings)
	if err != nil {
		return 0, fmt.Errorf("confirm transferred postings: %w", err)
	}

	return confirmed, nil
}
