package dhtexchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/indextransfer"
)

type observedDistributions struct {
	receipts []DistributionReceipt
}

func (o *observedDistributions) Observe(receipt DistributionReceipt) {
	o.receipts = append(o.receipts, receipt)
}

func TestOutboundSchedulerSendsReadyChunkAndObservesSuccess(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "WWWWWWWWWWWW")
	peer := queueSeed(t, "AAAAAAAAAAAA")
	queue := NewOutboundQueue()
	queue.add(peer, []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
	})
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			queue,
			&capacityScript{count: 11},
			&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{}),
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig()},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Distribution.State != DistributionSent ||
		receipt.Distribution.Peer != peer.Hash ||
		receipt.Retry.Status != OutboundRetryCleared {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(observer.receipts) != 1 || observer.receipts[0].State != DistributionSent {
		t.Fatalf("observed = %#v", observer.receipts)
	}
}

func TestOutboundSchedulerDefersDelayedPeers(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	word := queueHash(t, "WWWWWWWWWWWW")
	peer := queueSeed(t, "BBBBBBBBBBBB")
	queue := NewOutboundQueue()
	queue.add(peer, []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
	})
	retry := NewOutboundRetryPolicy(OutboundRetryConfig{
		BaseDelay:          time.Minute,
		MaxDelay:           time.Hour,
		QuarantineFailures: 3,
		QuarantineDuration: time.Hour,
		DelayFraction:      func(yacymodel.Hash, int) float64 { return 0.5 },
	})
	retry.Observe(DistributionReceipt{State: DistributionCapacityFailed, Peer: peer.Hash}, at)
	probe := &capacityScript{count: 11}
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(queue, probe, handoff),
		retry,
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{
			Gates: DefaultGateConfig(),
			Now:   func() time.Time { return at },
		},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Distribution.State != DistributionRetryDeferred ||
		receipt.Retry.Status != OutboundRetryIgnored {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 || probe.calls != 0 || handoff.calls != 0 {
		t.Fatalf(
			"queue/probe/handoff = %d/%d/%d",
			queue.PostingCount(),
			probe.calls,
			handoff.calls,
		)
	}
	if len(observer.receipts) != 1 || observer.receipts[0].State != DistributionRetryDeferred {
		t.Fatalf("observed = %#v", observer.receipts)
	}
}

func TestOutboundSchedulerObservesFailureAndRetryDecision(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	word := queueHash(t, "WWWWWWWWWWWW")
	peer := queueSeed(t, "CCCCCCCCCCCC")
	queue := NewOutboundQueue()
	queue.add(peer, []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
	})
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			queue,
			&capacityScript{err: errors.New("capacity failed")},
			&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{
			BaseDelay:          time.Minute,
			MaxDelay:           time.Hour,
			QuarantineFailures: 3,
			QuarantineDuration: time.Hour,
			DelayFraction:      func(yacymodel.Hash, int) float64 { return 0.5 },
		}),
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{
			Gates: DefaultGateConfig(),
			Now:   func() time.Time { return at },
		},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected capacity error")
	}
	if receipt.Distribution.State != DistributionCapacityFailed ||
		receipt.Retry.Status != OutboundRetryDelayed ||
		receipt.Retry.Delay != time.Minute ||
		queue.PostingCount() != 1 {
		t.Fatalf("receipt = %#v queue=%d", receipt, queue.PostingCount())
	}
	if len(observer.receipts) != 1 || observer.receipts[0].State != DistributionCapacityFailed {
		t.Fatalf("observed = %#v", observer.receipts)
	}
}
