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
	if network.gates.snapshot == nil {
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
		if status.PublicReachable {
			s.recorder.Record(
				events.SeverityInfo,
				events.CategoryP2P,
				publicEndpointReachableEvent,
				"public endpoint self-test confirmed reachability",
			)
		} else {
			s.recorder.Record(
				events.SeverityWarn,
				events.CategoryP2P,
				publicEndpointUnreachableEvent,
				"public endpoint self-test could not confirm reachability",
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
