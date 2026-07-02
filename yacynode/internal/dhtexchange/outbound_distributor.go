package dhtexchange

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/indextransfer"
)

type RemoteCapacity interface {
	RWICount(ctx context.Context, peer yacymodel.Seed) (int, error)
}

type IndexHandoff interface {
	Send(
		ctx context.Context,
		peer yacymodel.Seed,
		postings []yacymodel.RWIPosting,
	) (indextransfer.HandoffReceipt, error)
}

type SentPostingConfirmer interface {
	ConfirmTransferred(ctx context.Context, postings []yacymodel.RWIPosting) (int, error)
}

type DistributionState string

const (
	DistributionGateClosed      DistributionState = "gate_closed"
	DistributionQueueEmpty      DistributionState = "queue_empty"
	DistributionCapacityFailed  DistributionState = "capacity_failed"
	DistributionHandoffFailed   DistributionState = "handoff_failed"
	DistributionHandoffRejected DistributionState = "handoff_rejected"
	DistributionRetryDeferred   DistributionState = "retry_deferred"
	DistributionSent            DistributionState = "sent"
)

type DistributionReceipt struct {
	State             DistributionState
	Gates             GateReport
	Peer              yacymodel.Hash
	PostingCount      int
	RemoteRWIWords    int
	Handoff           indextransfer.HandoffReceipt
	RequeuedPostings  int
	ConfirmedPostings int
}

type OutboundDistributor struct {
	queue     *OutboundQueue
	probe     RemoteCapacity
	handoff   IndexHandoff
	confirmer SentPostingConfirmer
}

func NewOutboundDistributor(
	queue *OutboundQueue,
	probe RemoteCapacity,
	handoff IndexHandoff,
) OutboundDistributor {
	return OutboundDistributor{queue: queue, probe: probe, handoff: handoff}
}

func NewConfirmingOutboundDistributor(
	queue *OutboundQueue,
	probe RemoteCapacity,
	handoff IndexHandoff,
	confirmer SentPostingConfirmer,
) OutboundDistributor {
	return OutboundDistributor{
		queue:     queue,
		probe:     probe,
		handoff:   handoff,
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
	receipt.PostingCount = len(chunk.Postings)

	count, err := d.probe.RWICount(ctx, chunk.Peer)
	receipt.RemoteRWIWords = count
	if err != nil {
		receipt.State = DistributionCapacityFailed
		receipt.RequeuedPostings = d.queue.Requeue(chunk)

		return receipt, fmt.Errorf("probe remote rwi count: %w", err)
	}

	handoff, err := d.handoff.Send(ctx, chunk.Peer, chunk.Postings)
	receipt.Handoff = handoff
	if err != nil {
		receipt.State = DistributionHandoffFailed
		receipt.RequeuedPostings = d.queue.Requeue(chunk)

		return receipt, fmt.Errorf("send dht chunk: %w", err)
	}
	if !handoffAccepted(handoff.State) {
		receipt.State = DistributionHandoffRejected
		receipt.RequeuedPostings = d.queue.Requeue(chunk)

		return receipt, nil
	}

	receipt.State = DistributionSent
	if d.confirmer != nil {
		confirmed, err := d.confirmer.ConfirmTransferred(ctx, chunk.Postings)
		receipt.ConfirmedPostings = confirmed
		if err != nil {
			return receipt, fmt.Errorf("confirm sent dht chunk: %w", err)
		}
	}

	return receipt, nil
}

func handoffAccepted(state indextransfer.HandoffState) bool {
	return state == indextransfer.HandoffRWIOnly || state == indextransfer.HandoffURLSent
}
