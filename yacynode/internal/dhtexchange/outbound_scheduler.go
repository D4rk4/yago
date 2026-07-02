package dhtexchange

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
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
	var feed OutboundFeedReceipt
	if s.config.Feed != nil {
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
		s.gates(ctx),
		s.config.Gates,
		func(peer yacymodel.Hash) bool { return s.retry.Ready(peer, at) },
	)
	s.observer.Observe(receipt)
	retry := s.retry.Observe(receipt, at)

	return ScheduledDistributionReceipt{Feed: feed, Distribution: receipt, Retry: retry}, err
}
