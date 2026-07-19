package crawlorder

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestHeartbeatAppliesTypedCrawlerRuntimePolicy(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.CrawlDelay = 250 * time.Millisecond
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	client := &storageHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: t.Context()},
		result:       &crawlrpc.WorkerHeartbeatResult{RuntimePolicy: message},
	}
	var applied yagocrawlcontract.CrawlerRuntimePolicy
	config := grpcOrderReceiverConfig{}
	WithHeartbeatRuntimePolicy(
		func() yagocrawlcontract.CrawlerRuntimePolicy { return policy },
		func(value yagocrawlcontract.CrawlerRuntimePolicy) { applied = value },
	)(&config)
	if !config.runtimePolicySource().Equal(policy) {
		t.Fatalf("effective runtime policy = %+v, want %+v", config.runtimePolicySource(), policy)
	}
	delivery := heartbeatDelivery{
		client:              client,
		workerID:            "worker",
		runtimePolicySource: config.runtimePolicySource,
		runtimePolicy:       config.runtimePolicy,
	}
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("exchange heartbeat: %v", err)
	}
	if !applied.Equal(policy) {
		t.Fatalf("applied policy = %+v, want %+v", applied, policy)
	}

	client.result.RuntimePolicy = &crawlrpc.CrawlerRuntimePolicy{}
	if _, err := delivery.exchange(t.Context(), nil); err == nil {
		t.Fatal("invalid heartbeat policy accepted")
	}
}

func TestHeartbeatLegacyRuntimePolicyPreservesEffectiveFacilities(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	message.BrowserSandbox = nil
	message.BrowserPath = nil
	message.MetricsAddress = nil
	client := &storageHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: t.Context()},
		result:       &crawlrpc.WorkerHeartbeatResult{RuntimePolicy: message},
	}
	effective := policy
	effective.BrowserSandbox = true
	effective.BrowserPath = "/usr/bin/firefox-esr"
	effective.MetricsAddress = "127.0.0.1:9101"
	var applied yagocrawlcontract.CrawlerRuntimePolicy
	delivery := heartbeatDelivery{
		client:              client,
		workerID:            "worker",
		runtimePolicySource: func() yagocrawlcontract.CrawlerRuntimePolicy { return effective },
		runtimePolicy:       func(value yagocrawlcontract.CrawlerRuntimePolicy) { applied = value },
	}
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("exchange heartbeat: %v", err)
	}
	if !applied.BrowserSandbox {
		t.Fatal("legacy heartbeat erased the effective sandbox opt-in")
	}
	if applied.BrowserPath != effective.BrowserPath ||
		applied.MetricsAddress != effective.MetricsAddress {
		t.Fatalf("legacy heartbeat facilities = %+v, want %+v", applied, effective)
	}
}
