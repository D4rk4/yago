package dhtexchange

import (
	"context"
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
}

type ScheduledDistributionReceipt struct {
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
	receipt, err := s.distributor.DistributeReady(
		ctx,
		s.gates(ctx),
		s.config.Gates,
		func(peer yacymodel.Hash) bool { return s.retry.Ready(peer, at) },
	)
	s.observer.Observe(receipt)
	retry := s.retry.Observe(receipt, at)

	return ScheduledDistributionReceipt{Distribution: receipt, Retry: retry}, err
}
