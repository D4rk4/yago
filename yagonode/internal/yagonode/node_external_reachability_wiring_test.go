package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
)

func TestAssembleNodeSharesExternalReachabilityEvidenceWithDHT(t *testing.T) {
	restoreAssemblySeams(t)
	var evidence *peerannouncement.ExternalReachabilityEvidence
	assembleRuntimePeerExchange = func(exchange peerExchange) (peerExchangeRuntime, error) {
		evidence = exchange.externalReachabilityEvidence

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

	assembled, err := assembleNode(
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
	if evidence == nil || assembled.peerType != evidence {
		t.Fatalf("published peer classification = %T, want shared evidence", assembled.peerType)
	}
	peerType, known := assembled.report.SelfSeed(t.Context()).PeerType.Get()
	if !known || peerType != yagomodel.PeerVirgin {
		t.Fatalf("initial self peer type = %q known=%t, want virgin", peerType, known)
	}
	evidence.Observe(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.PeerSenior)
	peerType, known = assembled.report.SelfSeed(t.Context()).PeerType.Get()
	if !known || peerType != yagomodel.PeerSenior {
		t.Fatalf("observed self peer type = %q known=%t, want senior", peerType, known)
	}
}
