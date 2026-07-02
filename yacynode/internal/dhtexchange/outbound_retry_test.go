package dhtexchange

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

func TestOutboundRetryPolicyIgnoresNonWorkStatesAndUsesZeroConfigDefaults(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueHash(t, "AAAAAAAAAAAA")
	policy := NewOutboundRetryPolicy(OutboundRetryConfig{})

	ignored := policy.Observe(
		DistributionReceipt{State: DistributionGateClosed, Peer: peer},
		at,
	)
	if ignored.Status != OutboundRetryIgnored || ignored.Peer != peer {
		t.Fatalf("ignored = %#v", ignored)
	}
	if !policy.Ready(peer, at) {
		t.Fatal("peer without retry state should be ready")
	}

	first := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	second := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	third := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	if first.Delay != time.Minute ||
		second.Delay != 2*time.Minute ||
		third.Delay != 4*time.Minute ||
		third.Status != OutboundRetryQuarantined ||
		!third.QuarantineUntil.Equal(at.Add(DefaultOutboundRetryQuarantineDuration)) {
		t.Fatalf("decisions = %#v %#v %#v", first, second, third)
	}
}

func TestOutboundRetryPolicyDelaysFailuresWithJitter(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueHash(t, "BBBBBBBBBBBB")
	config := DefaultOutboundRetryConfig()
	config.BaseDelay = time.Minute
	config.MaxDelay = time.Hour
	config.JitterRatio = 0.5
	config.QuarantineFailures = 4
	config.DelayFraction = func(yacymodel.Hash, int) float64 { return 0.25 }
	policy := NewOutboundRetryPolicy(config)

	first := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	second := policy.Observe(
		DistributionReceipt{State: DistributionHandoffFailed, Peer: peer},
		at,
	)
	if first.Status != OutboundRetryDelayed ||
		first.Delay != 37500*time.Millisecond ||
		!first.RetryAfter.Equal(at.Add(37500*time.Millisecond)) ||
		second.Failures != 2 ||
		second.Delay != 75*time.Second {
		t.Fatalf("decisions = %#v %#v", first, second)
	}

	state, ok := policy.PeerState(peer)
	if !ok || state.Failures != 2 || !state.RetryAfter.Equal(second.RetryAfter) {
		t.Fatalf("state = %#v ok=%t", state, ok)
	}
	if policy.Ready(peer, second.RetryAfter.Add(-time.Nanosecond)) {
		t.Fatal("peer should wait until retry deadline")
	}
	if !policy.Ready(peer, second.RetryAfter) {
		t.Fatal("peer should be ready at retry deadline")
	}
}

func TestOutboundRetryPolicyQuarantinesAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueHash(t, "CCCCCCCCCCCC")
	policy := NewOutboundRetryPolicy(OutboundRetryConfig{
		BaseDelay:          time.Minute,
		MaxDelay:           10 * time.Minute,
		QuarantineFailures: 2,
		QuarantineDuration: 15 * time.Minute,
		DelayFraction:      func(yacymodel.Hash, int) float64 { return 0.5 },
	})

	delayed := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	quarantined := policy.Observe(
		DistributionReceipt{State: DistributionHandoffRejected, Peer: peer},
		at,
	)
	if delayed.Status != OutboundRetryDelayed ||
		quarantined.Status != OutboundRetryQuarantined ||
		quarantined.Delay != 2*time.Minute ||
		!quarantined.QuarantineUntil.Equal(at.Add(15*time.Minute)) {
		t.Fatalf("decisions = %#v %#v", delayed, quarantined)
	}
	if policy.Ready(peer, at.Add(3*time.Minute)) {
		t.Fatal("peer should stay quarantined after retry delay")
	}
	if !policy.Ready(peer, at.Add(15*time.Minute)) {
		t.Fatal("peer should leave quarantine at the quarantine deadline")
	}
}

func TestOutboundRetryPolicyClearsPeerAfterSuccess(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueHash(t, "DDDDDDDDDDDD")
	policy := NewOutboundRetryPolicy(OutboundRetryConfig{
		BaseDelay:          time.Minute,
		MaxDelay:           time.Hour,
		QuarantineFailures: 2,
		QuarantineDuration: time.Hour,
		DelayFraction:      func(yacymodel.Hash, int) float64 { return 0.5 },
	})
	policy.Observe(DistributionReceipt{State: DistributionCapacityFailed, Peer: peer}, at)
	if policy.Ready(peer, at) {
		t.Fatal("peer should be delayed before success")
	}

	cleared := policy.Observe(DistributionReceipt{State: DistributionSent, Peer: peer}, at)
	if cleared.Status != OutboundRetryCleared || cleared.Peer != peer {
		t.Fatalf("cleared = %#v", cleared)
	}
	if _, ok := policy.PeerState(peer); ok {
		t.Fatal("success should remove retry state")
	}
	if !policy.Ready(peer, at) {
		t.Fatal("peer should be ready after success")
	}
}

func TestOutboundRetryPolicyCapsDelayAndClampsFractions(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueHash(t, "EEEEEEEEEEEE")
	policy := NewOutboundRetryPolicy(OutboundRetryConfig{
		BaseDelay:          time.Minute,
		MaxDelay:           3 * time.Minute,
		JitterRatio:        2,
		QuarantineFailures: 10,
		QuarantineDuration: time.Hour,
		DelayFraction:      func(yacymodel.Hash, int) float64 { return 2 },
	})

	policy.Observe(DistributionReceipt{State: DistributionCapacityFailed, Peer: peer}, at)
	policy.Observe(DistributionReceipt{State: DistributionCapacityFailed, Peer: peer}, at)
	third := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	if third.Delay != 3*time.Minute || third.Status != OutboundRetryDelayed {
		t.Fatalf("third = %#v", third)
	}
}

func TestOutboundRetryPolicyClampsNegativeConfig(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueHash(t, "FFFFFFFFFFFF")
	policy := NewOutboundRetryPolicy(OutboundRetryConfig{
		BaseDelay:          time.Hour,
		MaxDelay:           time.Minute,
		JitterRatio:        -1,
		QuarantineFailures: 5,
		QuarantineDuration: time.Hour,
		DelayFraction:      func(yacymodel.Hash, int) float64 { return -1 },
	})

	decision := policy.Observe(
		DistributionReceipt{State: DistributionCapacityFailed, Peer: peer},
		at,
	)
	if decision.Delay != time.Hour || !decision.RetryAfter.Equal(at.Add(time.Hour)) {
		t.Fatalf("decision = %#v", decision)
	}
}
