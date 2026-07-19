package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

func TestAssembleNodeMountsEnabledRemoteCrawlCompatibility(t *testing.T) {
	config := testConfig(t)
	config.RemoteCrawl.Enabled = true
	config.RemoteCrawl.TrustedPeers = []yagomodel.Hash{config.Hash}
	config.RemoteCrawl.AllowedDestinations = []string{"example.com"}
	assembled := assembleTestNode(t, config, openTestVault(t))
	if assembled.peerMux == nil {
		t.Fatal("enabled remote crawl produced no peer routes")
	}
}

func TestAssembleNodeLeavesRemoteCrawlObserverDetachedWhenDisabled(t *testing.T) {
	config := testConfig(t)
	crawl, err := loadRuntimeCrawlConfig(func(string) string { return "" }, config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	config.Crawl = crawl
	assembled := assembleTestNode(t, config, openTestVault(t))
	runtime, ok := assembled.crawl.(*crawlRuntime)
	if !ok {
		t.Fatalf("crawl runtime = %T", assembled.crawl)
	}
	if runtime.remoteCrawl != nil {
		t.Fatal("disabled remote crawl attached an order observer")
	}
}

func TestAssembleNodeRejectsInvalidRemoteCrawlBrokerConfiguration(t *testing.T) {
	config := testConfig(t)
	config.RemoteCrawl.QueueCapacity = remotecrawl.MaximumQueueCapacity + 1
	_, err := assembleNode(
		context.Background(),
		config,
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if err == nil {
		t.Fatal("invalid remote crawl broker configuration accepted")
	}
}
