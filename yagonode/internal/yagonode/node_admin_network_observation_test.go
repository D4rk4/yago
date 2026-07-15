package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func TestNetworkSourceReportsUnknownGateState(t *testing.T) {
	config := dhtexchange.DefaultGateConfig()
	config.MinimumConnectedPeer = 1
	config.MinimumRWIWord = 1
	config.AllowWhileIndexing = false
	source := newNetworkSource(dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{
				PublicReachable: true,
				LocalPeerKnown:  true,
				ConnectedPeers:  1,
			}
		},
		config: config,
	}, nil, nil, nil, nil)

	status := source.Network(t.Context())
	if !status.Available || status.DHTOpen ||
		status.BlockingReason != dhtexchange.GateLocalRWIUnavailableReason {
		t.Fatalf("status = %+v", status)
	}
	wants := map[string]string{
		string(dhtexchange.GateLocalRWI):         dhtexchange.GateLocalRWIUnavailableReason,
		string(dhtexchange.GateCrawlIdle):        dhtexchange.GateCrawlQueueUnavailableReason,
		string(dhtexchange.GateIndexIdle):        dhtexchange.GateIndexQueueUnavailableReason,
		string(dhtexchange.GateStorageAvailable): dhtexchange.GateStorageStatusUnavailableReason,
	}
	for _, gate := range status.Gates {
		if reason, ok := wants[gate.Name]; ok {
			if gate.Open || gate.Reason != reason {
				t.Fatalf("gate %q = %+v, want closed reason %q", gate.Name, gate, reason)
			}
			delete(wants, gate.Name)
		}
	}
	if len(wants) != 0 {
		t.Fatalf("missing gates = %+v", wants)
	}
}
