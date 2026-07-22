package dhtexchange

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type DistributionObserver interface {
	Observe(DistributionReceipt)
}

type GateStateSnapshot func(context.Context) GateState

type OutboundSchedulerConfig struct {
	Gates GateConfig
	Now   func() time.Time
	Feed  OutboundQueueFeeder
}

type ScheduledDistributionReceipt struct {
	Feed         OutboundFeedReceipt
	Distribution DistributionReceipt
	Retry        OutboundRetryDecision
}

type OutboundScheduler struct {
	distributor OutboundDistributor
	retry       *OutboundRetryPolicy
	observer    DistributionObserver
	gates       GateStateSnapshot
	config      OutboundSchedulerConfig
}

func NewOutboundScheduler(
	distributor OutboundDistributor,
	retry *OutboundRetryPolicy,
	observer DistributionObserver,
	gates GateStateSnapshot,
	config OutboundSchedulerConfig,
) OutboundScheduler {
	if config.Now == nil {
		config.Now = time.Now
	}

	return OutboundScheduler{
		distributor: distributor,
		retry:       retry,
		observer:    observer,
		gates:       gates,
		config:      config,
	}
}

func (s OutboundScheduler) RunOnce(
	ctx context.Context,
) (ScheduledDistributionReceipt, error) {
	at := s.config.Now()
	state := s.gates(ctx)
	var feed OutboundFeedReceipt
	if s.config.Feed != nil && EvaluateGates(state, s.config.Gates).Open &&
		!s.distributor.queue.localCompletionPending() {
		var err error
		feed, err = s.config.Feed.Feed(ctx)
		if err != nil {
			return ScheduledDistributionReceipt{Feed: feed}, fmt.Errorf(
				"feed outbound queue: %w",
				err,
			)
		}
	}
	receipt, err := s.distributor.DistributeReady(
		ctx,
		state,
		s.config.Gates,
		func(peer yagomodel.Hash) bool { return s.retry.Ready(peer, at) },
	)
	retry := s.retry.Observe(receipt, at)
	if receipt.State == DistributionHandoffFailed &&
		retry.Status == OutboundRetryQuarantined {
		restored, requeued, restoreErr := s.distributor.RestoreRequeuedPeer(ctx, receipt.Peer)
		receipt.RestoredPostings = restored
		receipt.RequeuedPostings = requeued
		if restoreErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf("restore repeatedly failed dht chunk: %w", restoreErr),
			)
		}
	}
	s.observer.Observe(receipt)

	return ScheduledDistributionReceipt{Feed: feed, Distribution: receipt, Retry: retry}, err
}
