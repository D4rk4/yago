package dhtexchange

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

type IndexHandoff interface {
	Send(
		ctx context.Context,
		peer yagomodel.Seed,
		postings []yagomodel.RWIPosting,
	) (indextransfer.HandoffReceipt, error)
}

type SentPostingConfirmer interface {
	ConfirmTransferred(ctx context.Context, postings []yagomodel.RWIPosting) (int, error)
}

type DistributionState string

const (
	DistributionGateClosed      DistributionState = "gate_closed"
	DistributionQueueEmpty      DistributionState = "queue_empty"
	DistributionHandoffFailed   DistributionState = "handoff_failed"
	DistributionHandoffRejected DistributionState = "handoff_rejected"
	DistributionRetryDeferred   DistributionState = "retry_deferred"
	DistributionSent            DistributionState = "sent"
)

type DistributionReceipt struct {
	State             DistributionState
	Gates             GateReport
	Peer              yagomodel.Hash
	Target            yagomodel.Seed
	PostingCount      int
	Handoff           indextransfer.HandoffReceipt
	RequeuedPostings  int
	RestoredPostings  int
	ConfirmedPostings int
}

type OutboundDistributor struct {
	queue     *OutboundQueue
	handoff   IndexHandoff
	restorer  OutboundWordRestorer
	confirmer SentPostingConfirmer
}

func NewOutboundDistributor(
	queue *OutboundQueue,
	handoff IndexHandoff,
	restorer OutboundWordRestorer,
) OutboundDistributor {
	return OutboundDistributor{queue: queue, handoff: handoff, restorer: restorer}
}

func NewConfirmingOutboundDistributor(
	queue *OutboundQueue,
	handoff IndexHandoff,
	restorer OutboundWordRestorer,
	confirmer SentPostingConfirmer,
) OutboundDistributor {
	return OutboundDistributor{
		queue:     queue,
		handoff:   handoff,
		restorer:  restorer,
		confirmer: confirmer,
	}
}

func (d OutboundDistributor) Distribute(
	ctx context.Context,
	state GateState,
	config GateConfig,
) (DistributionReceipt, error) {
	return d.distribute(ctx, state, config, d.queue.DequeueLargest)
}

func (d OutboundDistributor) DistributeReady(
	ctx context.Context,
	state GateState,
	config GateConfig,
	ready OutboundPeerReady,
) (DistributionReceipt, error) {
	return d.distribute(
		ctx,
		state,
		config,
		func() (OutboundChunk, bool) { return d.queue.DequeueLargestReady(ready) },
	)
}

func (d OutboundDistributor) distribute(
	ctx context.Context,
	state GateState,
	config GateConfig,
	dequeue func() (OutboundChunk, bool),
) (DistributionReceipt, error) {
	gates := EvaluateGates(state, config)
	receipt := DistributionReceipt{Gates: gates}
	if completion, pending, err := d.retryLocalCompletion(ctx, gates); pending {
		return completion, err
	}
	if !gates.Open {
		receipt.State = DistributionGateClosed

		return receipt, nil
	}

	chunk, ok := dequeue()
	if !ok {
		receipt.State = DistributionQueueEmpty
		if d.queue.Len() > 0 {
			receipt.State = DistributionRetryDeferred
		}

		return receipt, nil
	}

	receipt.Peer = chunk.Peer.Hash
	receipt.Target = chunk.Peer
	receipt.PostingCount = len(chunk.Postings)

	handoff, err := d.handoff.Send(ctx, chunk.Peer, chunk.Postings)
	receipt.Handoff = handoff
	if err != nil && !handoffRejected(handoff.State) {
		receipt.State = DistributionHandoffFailed
		receipt.RequeuedPostings = d.queue.Requeue(chunk)

		return receipt, fmt.Errorf("send dht chunk: %w", err)
	}
	if !handoffAccepted(handoff.State) {
		return d.finishRejectedHandoff(ctx, receipt, chunk, err)
	}

	return d.finishAcceptedHandoff(ctx, receipt, chunk, handoff)
}

func (d OutboundDistributor) RestoreRequeuedPeer(
	ctx context.Context,
	peer yagomodel.Hash,
) (restored int, requeued int, err error) {
	chunk, known := d.queue.DequeuePeer(peer)
	if !known {
		return 0, 0, nil
	}
	d.queue.cancelPostingCopies(chunk.Postings)

	return d.restoreChunk(ctx, chunk)
}

func (d OutboundDistributor) restoreChunk(
	ctx context.Context,
	chunk OutboundChunk,
) (restored int, requeued int, err error) {
	return d.restorePostings(ctx, chunk.Peer, chunk.Postings)
}

func (d OutboundDistributor) restorePostings(
	ctx context.Context,
	peer yagomodel.Seed,
	postings []yagomodel.RWIPosting,
) (restored int, requeued int, err error) {
	restored, err = d.restorer.RestoreOutboundWords(ctx, outboundRestoreWords(postings))
	if err != nil {
		requeued = d.queue.Requeue(OutboundChunk{Peer: peer, Postings: postings})

		return 0, requeued, fmt.Errorf("restore outbound words: %w", err)
	}

	return restored, 0, nil
}

func handoffAccepted(state indextransfer.HandoffState) bool {
	return state == indextransfer.HandoffRWIOnly || state == indextransfer.HandoffURLSent
}

func handoffRejected(state indextransfer.HandoffState) bool {
	return state == indextransfer.HandoffRWIRejected || state == indextransfer.HandoffURLRejected
}
