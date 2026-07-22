package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

func TestNetworkSelfTesterReturnsFreshNetworkSnapshot(t *testing.T) {
	reachable := false
	network := newNetworkSource(dhtGateStatusSource{
		snapshotWithReachability: func(context.Context) (
			dhtexchange.GateState,
			publicReachabilitySnapshot,
		) {
			state := publicReachabilityUnreachable
			if reachable {
				state = publicReachabilityReachable
			}

			return dhtexchange.GateState{}, publicReachabilitySnapshot{state: state}
		},
	}, nil, nil, nil, nil)
	recorder := events.NewRecorder(4)
	tester := newNetworkSelfTester(network, recorder)
	if tester == nil {
		t.Fatal("self-test source unavailable")
	}

	first, err := tester.TestPublicEndpoint(t.Context())
	if err != nil || first.PublicReachable || !first.PublicReachabilityKnown {
		t.Fatalf("unreachable result = %+v, err = %v", first, err)
	}
	reachable = true
	second, err := tester.TestPublicEndpoint(t.Context())
	if err != nil || !second.PublicReachable {
		t.Fatalf("reachable result = %+v, err = %v", second, err)
	}
	recent := recorder.Recent(2)
	if len(recent) != 2 || recent[0].Name != publicEndpointReachableEvent ||
		recent[1].Name != publicEndpointUnreachableEvent {
		t.Fatalf("events = %+v", recent)
	}
}

func TestNetworkSelfTesterRecordsUnconfirmedEnrichedSnapshot(t *testing.T) {
	recorder := events.NewRecorder(2)
	tester := newNetworkSelfTester(newNetworkSource(dhtGateStatusSource{
		snapshotWithReachability: func(context.Context) (
			dhtexchange.GateState,
			publicReachabilitySnapshot,
		) {
			return dhtexchange.GateState{}, publicReachabilitySnapshot{}
		},
	}, nil, nil, nil, nil), recorder)
	status, err := tester.TestPublicEndpoint(t.Context())
	if err != nil || status.PublicReachabilityKnown {
		t.Fatalf("unconfirmed status = %+v, %v", status, err)
	}
	recent := recorder.Recent(1)
	if len(recent) != 1 || recent[0].Name != publicEndpointUnconfirmedEvent {
		t.Fatalf("events = %+v", recent)
	}
}

func TestNetworkSelfTesterRequiresProbeAndRecordsCancellation(t *testing.T) {
	if tester := newNetworkSelfTester(
		newNetworkSource(dhtGateStatusSource{}, nil, nil, nil, nil),
		nil,
	); tester != nil {
		t.Fatal("self-test source created without a probe")
	}
	recorder := events.NewRecorder(4)
	tester := networkSelfTester{
		network: newNetworkSource(dhtGateStatusSource{
			snapshot: func(context.Context) dhtexchange.GateState { return dhtexchange.GateState{} },
		}, nil, nil, nil, nil),
		recorder: recorder,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := tester.TestPublicEndpoint(ctx); err == nil {
		t.Fatal("canceled self-test succeeded")
	}
	recent := recorder.Recent(1)
	if len(recent) != 1 || recent[0].Name != publicEndpointTestFailedEvent {
		t.Fatalf("events = %+v", recent)
	}
	tester.recorder = nil
	if _, err := tester.TestPublicEndpoint(ctx); err == nil {
		t.Fatal("canceled self-test without recorder succeeded")
	}
}
