package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
)

type recordingDHTRoster struct {
	reachable           []yacymodel.Hash
	unreachable         []yacymodel.Hash
	remoteIndexRejected []yacymodel.Seed
}

func (r *recordingDHTRoster) ConfirmReachable(_ context.Context, peer yacymodel.Hash) {
	r.reachable = append(r.reachable, peer)
}

func (r *recordingDHTRoster) ConfirmUnreachable(_ context.Context, peer yacymodel.Hash) {
	r.unreachable = append(r.unreachable, peer)
}

func (r *recordingDHTRoster) RejectRemoteIndex(_ context.Context, peer yacymodel.Seed) {
	r.remoteIndexRejected = append(r.remoteIndexRejected, peer)
}

func TestDHTOutboundRosterCycleConfirmsSentPeer(t *testing.T) {
	t.Parallel()

	peer := yacymodel.Hash("AAAAAAAAAAAA")
	roster := &recordingDHTRoster{}
	receipt, err := (dhtOutboundRosterCycle{
		cycle: &scriptedDHTOutboundCycle{
			receipt: dhtexchange.ScheduledDistributionReceipt{
				Distribution: dhtexchange.DistributionReceipt{
					State: dhtexchange.DistributionSent,
					Peer:  peer,
				},
				Retry: dhtexchange.OutboundRetryDecision{
					Status: dhtexchange.OutboundRetryCleared,
					Peer:   peer,
				},
			},
		},
		roster: roster,
	}).RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Distribution.State != dhtexchange.DistributionSent ||
		len(roster.reachable) != 1 ||
		roster.reachable[0] != peer ||
		len(roster.unreachable) != 0 {
		t.Fatalf("receipt/roster = %#v/%#v", receipt, roster)
	}
}

func TestDHTOutboundRosterCycleQuarantinesPeerOnRepeatedFailure(t *testing.T) {
	t.Parallel()

	peer := yacymodel.Hash("BBBBBBBBBBBB")
	sentinel := errors.New("distribution failed")
	roster := &recordingDHTRoster{}
	at := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	receipt, err := (dhtOutboundRosterCycle{
		cycle: &scriptedDHTOutboundCycle{
			receipt: dhtexchange.ScheduledDistributionReceipt{
				Distribution: dhtexchange.DistributionReceipt{
					State: dhtexchange.DistributionCapacityFailed,
					Peer:  peer,
				},
				Retry: dhtexchange.OutboundRetryDecision{
					Status:          dhtexchange.OutboundRetryQuarantined,
					Peer:            peer,
					Failures:        3,
					RetryAfter:      at.Add(time.Minute),
					QuarantineUntil: at.Add(time.Hour),
				},
			},
			err: sentinel,
		},
		roster: roster,
	}).RunOnce(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunOnce error = %v, want %v", err, sentinel)
	}
	if receipt.Retry.Status != dhtexchange.OutboundRetryQuarantined ||
		len(roster.unreachable) != 1 ||
		roster.unreachable[0] != peer ||
		len(roster.reachable) != 0 {
		t.Fatalf("receipt/roster = %#v/%#v", receipt, roster)
	}
}

func TestDHTOutboundRosterCycleRejectsRemoteIndexOnRejectedHandoff(t *testing.T) {
	t.Parallel()

	target := dhtOutboundPeer(t)
	roster := &recordingDHTRoster{}
	receipt, err := (dhtOutboundRosterCycle{
		cycle: &scriptedDHTOutboundCycle{
			receipt: dhtexchange.ScheduledDistributionReceipt{
				Distribution: dhtexchange.DistributionReceipt{
					State:  dhtexchange.DistributionHandoffRejected,
					Peer:   target.Hash,
					Target: target,
				},
				Retry: dhtexchange.OutboundRetryDecision{
					Status: dhtexchange.OutboundRetryDelayed,
					Peer:   target.Hash,
				},
			},
		},
		roster: roster,
	}).RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Distribution.State != dhtexchange.DistributionHandoffRejected ||
		len(roster.remoteIndexRejected) != 1 ||
		roster.remoteIndexRejected[0].Hash != target.Hash ||
		len(roster.unreachable) != 0 {
		t.Fatalf("receipt/roster = %#v/%#v", receipt, roster)
	}
}

func TestDHTOutboundRosterCycleIgnoresPeerlessAndDelayedReceipts(t *testing.T) {
	t.Parallel()

	peer := yacymodel.Hash("CCCCCCCCCCCC")
	roster := &recordingDHTRoster{}
	cycle := dhtOutboundRosterCycle{roster: roster}
	cycle.observe(context.Background(), dhtexchange.ScheduledDistributionReceipt{})
	cycle.observe(context.Background(), dhtexchange.ScheduledDistributionReceipt{
		Distribution: dhtexchange.DistributionReceipt{
			State: dhtexchange.DistributionCapacityFailed,
			Peer:  peer,
		},
		Retry: dhtexchange.OutboundRetryDecision{
			Status: dhtexchange.OutboundRetryDelayed,
			Peer:   peer,
		},
	})

	if len(roster.reachable) != 0 || len(roster.unreachable) != 0 {
		t.Fatalf("roster = %#v, want no mutation", roster)
	}
}
