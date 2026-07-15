package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func TestAssembleNodeSharesHostLinkSnapshotWithPeerExchange(t *testing.T) {
	restoreAssemblySeams(t)
	var peerSnapshot hostlinks.IncomingHostLinks
	assembleRuntimePeerExchange = func(exchange peerExchange) (peerExchangeRuntime, error) {
		peerSnapshot = exchange.host

		return peerExchangeRuntime{announcer: fakeAnnouncer{}}, nil
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
		t.Fatal(err)
	}
	holder, ok := peerSnapshot.(*hostlinks.SnapshotHolder)
	if !ok || assembled.corpusPass == nil || holder != assembled.corpusPass.hostLinks {
		t.Fatalf("shared host-link snapshot = %T/%#v", peerSnapshot, assembled.corpusPass)
	}
}
