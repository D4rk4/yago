package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
	"github.com/D4rk4/yago/yagoproto"
)

type recordingDHTRoster struct {
	reachable           []yagomodel.Hash
	unreachable         []yagomodel.Hash
	remoteIndexRejected []yagomodel.Seed
}

func (r *recordingDHTRoster) ConfirmReachable(_ context.Context, peer yagomodel.Hash) {
	r.reachable = append(r.reachable, peer)
}

func (r *recordingDHTRoster) ConfirmUnreachable(_ context.Context, peer yagomodel.Hash) {
	r.unreachable = append(r.unreachable, peer)
}

func (r *recordingDHTRoster) RejectRemoteIndex(_ context.Context, peer yagomodel.Seed) {
	r.remoteIndexRejected = append(r.remoteIndexRejected, peer)
}

type rejectedDHTHandoffCase struct {
	name     string
	handoff  indextransfer.HandoffReceipt
	retry    dhtexchange.OutboundRetryStatus
	failures int
}

func rejectedRWIHandoff(
	result yagoproto.TransferRWIResult,
	pause int,
) indextransfer.HandoffReceipt {
	return indextransfer.HandoffReceipt{
		State: indextransfer.HandoffRWIRejected,
		RWI:   yagoproto.TransferRWIResponse{Result: result, Pause: pause},
	}
}

func rejectedURLHandoff(result yagoproto.TransferURLResult) indextransfer.HandoffReceipt {
	return indextransfer.HandoffReceipt{
		State: indextransfer.HandoffURLRejected,
		URL:   yagoproto.TransferURLResponse{Result: result},
	}
}

func rejectedDHTHandoffCases() []rejectedDHTHandoffCase {
	return []rejectedDHTHandoffCase{
		{
			name:    "rwi busy",
			handoff: rejectedRWIHandoff(yagoproto.ResultBusy, 9),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:     "rwi high load quarantined",
			handoff:  rejectedRWIHandoff(yagoproto.ResultTooHighLoad, 0),
			retry:    dhtexchange.OutboundRetryQuarantined,
			failures: 3,
		},
		{
			name:    "rwi not granted",
			handoff: rejectedRWIHandoff(yagoproto.ResultNotGranted, 0),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name: "rwi wrong target",
			handoff: rejectedRWIHandoff(
				yagoproto.TransferRWIResult(yagoproto.ResultWrongTarget),
				0,
			),
			retry: dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "rwi not authenticated",
			handoff: rejectedRWIHandoff(yagoproto.ResultNotAuthentified, 0),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "rwi receiver unavailable",
			handoff: rejectedRWIHandoff(yagoproto.ResultPostOrEnvIsNull, 0),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "rwi missing word count",
			handoff: rejectedRWIHandoff(yagoproto.ResultMissingWordC, 0),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "rwi missing entry count",
			handoff: rejectedRWIHandoff(yagoproto.ResultMissingEntryC, 0),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "rwi missing indexes",
			handoff: rejectedRWIHandoff(yagoproto.ResultMissingIndexes, 0),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "url not granted",
			handoff: rejectedURLHandoff(yagoproto.ResultErrorNotGranted),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "url wrong target",
			handoff: rejectedURLHandoff(yagoproto.TransferURLResult(yagoproto.ResultWrongTarget)),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
		{
			name:    "url empty auth response",
			handoff: rejectedURLHandoff(""),
			retry:   dhtexchange.OutboundRetryDelayed,
		},
	}
}

func TestDHTOutboundRosterCycleConfirmsSentPeer(t *testing.T) {
	t.Parallel()

	peer := yagomodel.Hash("AAAAAAAAAAAA")
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

	peer := yagomodel.Hash("BBBBBBBBBBBB")
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

func TestDHTOutboundRosterCyclePreservesAdvertisedCapabilityOnRejectedHandoff(t *testing.T) {
	t.Parallel()

	target := dhtOutboundPeer(t)
	for _, test := range rejectedDHTHandoffCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			roster := &recordingDHTRoster{}
			receipt, err := (dhtOutboundRosterCycle{
				cycle: &scriptedDHTOutboundCycle{
					receipt: dhtexchange.ScheduledDistributionReceipt{
						Distribution: dhtexchange.DistributionReceipt{
							State:   dhtexchange.DistributionHandoffRejected,
							Peer:    target.Hash,
							Target:  target,
							Handoff: test.handoff,
						},
						Retry: dhtexchange.OutboundRetryDecision{
							Status:   test.retry,
							Peer:     target.Hash,
							Failures: test.failures,
						},
					},
				},
				roster: roster,
			}).RunOnce(context.Background())
			if err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if receipt.Distribution.Handoff.State != test.handoff.State ||
				len(roster.remoteIndexRejected) != 0 ||
				len(roster.unreachable) != 0 ||
				len(roster.reachable) != 0 {
				t.Fatalf("receipt/roster = %#v/%#v", receipt, roster)
			}
		})
	}
}

func TestDHTOutboundRosterCycleIgnoresPeerlessAndDelayedReceipts(t *testing.T) {
	t.Parallel()

	peer := yagomodel.Hash("CCCCCCCCCCCC")
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
