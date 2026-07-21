package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
)

func TestAssembleNodeSharesExternalReachabilityEvidenceWithDHT(t *testing.T) {
	restoreAssemblySeams(t)
	evidence := peerannouncement.NewExternalReachabilityEvidence()
	assembleRuntimePeerExchange = func(peerExchange) (peerExchangeRuntime, error) {
		return peerExchangeRuntime{
			announcer:                    fakeAnnouncer{},
			externalReachabilityEvidence: evidence,
		}, nil
	}
	var received externalReachabilitySnapshots
	buildRuntimeDHTOutbound = func(assembly dhtOutboundRuntimeAssembly) dhtOutboundProcess {
		received = assembly.externalReachability

		return dhtOutboundProcess{}
	}

	_, err := assembleNode(
		context.Background(),
		testConfig(t),
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if err != nil {
		t.Fatalf("assemble node: %v", err)
	}
	if received != evidence {
		t.Fatalf("DHT external reachability = %T, want shared evidence", received)
	}
}
