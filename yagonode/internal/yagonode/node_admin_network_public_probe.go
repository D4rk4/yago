package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

const (
	publicEndpointReachableEvent   = "network.public_endpoint.reachable"
	publicEndpointUnreachableEvent = "network.public_endpoint.unreachable"
	publicEndpointUnconfirmedEvent = "network.public_endpoint.unconfirmed"
	publicEndpointTestFailedEvent  = "network.public_endpoint.test_failed"
)

type networkSelfTester struct {
	network  networkSource
	recorder *events.Recorder
}

func newNetworkSelfTester(
	network networkSource,
	recorder *events.Recorder,
) adminui.NetworkSelfTester {
	if network.gates.snapshot == nil && network.gates.snapshotWithReachability == nil {
		return nil
	}

	return networkSelfTester{network: network, recorder: recorder}
}

func (s networkSelfTester) TestPublicEndpoint(
	ctx context.Context,
) (adminui.NetworkStatus, error) {
	if err := ctx.Err(); err != nil {
		s.recordFailure()

		return adminui.NetworkStatus{}, fmt.Errorf("check public endpoint test context: %w", err)
	}
	status := s.network.Network(ctx)
	if s.recorder != nil {
		switch {
		case !status.PublicReachabilityKnown:
			s.recorder.Record(
				events.SeverityWarn,
				events.CategoryP2P,
				publicEndpointUnconfirmedEvent,
				"public endpoint reachability remains unconfirmed",
			)
		case status.PublicReachable:
			s.recorder.Record(
				events.SeverityInfo,
				events.CategoryP2P,
				publicEndpointReachableEvent,
				"public endpoint reachability check confirmed reachability",
			)
		default:
			s.recorder.Record(
				events.SeverityWarn,
				events.CategoryP2P,
				publicEndpointUnreachableEvent,
				"public endpoint reachability check reports unreachable",
			)
		}
	}

	return status, nil
}

func (s networkSelfTester) recordFailure() {
	if s.recorder == nil {
		return
	}
	s.recorder.Record(
		events.SeverityWarn,
		events.CategoryP2P,
		publicEndpointTestFailedEvent,
		"public endpoint self-test failed",
	)
}
