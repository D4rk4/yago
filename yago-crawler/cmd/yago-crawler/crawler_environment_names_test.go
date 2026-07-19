package main

import (
	"strings"
	"testing"
)

func TestCrawlerEnvironmentNamesUseCanonicalPrefix(t *testing.T) {
	names := []string{
		EnvNodeRPCAddr,
		EnvWorkerID,
		EnvWorkers,
		EnvProcessPagesPerSecond,
		EnvMaxActiveRuns,
		EnvPrioritizeAutomaticDiscovery,
		EnvMaxHostConcurrency,
		EnvMaxDepth,
		EnvMaxPagesPerRun,
		EnvRunPagesPerMinute,
		EnvCrawlDelay,
		EnvUserAgent,
		EnvRequestTimeout,
		EnvConnectTimeout,
		EnvTLSTimeout,
		EnvHeaderTimeout,
		EnvMaxRedirects,
		EnvSitemapURLLimit,
		EnvEgressAllowLAN,
		EnvEgressAllowCIDRs,
		EnvMetricsAddr,
		EnvShutdownGrace,
		EnvBrowserPath,
		EnvBrowserSandbox,
		EnvBrowserFailureThreshold,
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "YAGO_CRAWLER_") {
			t.Errorf("crawler environment name %q does not use the canonical prefix", name)
		}
	}
}

func TestJoinedLegacyCrawlerEnvironmentNameIsNotAccepted(t *testing.T) {
	_, err := LoadServiceConfig(envFrom(map[string]string{
		"YAGO" + "CRAWLER_NODE_RPC_ADDR": "node:9091",
	}))
	if err == nil {
		t.Fatal("joined legacy crawler environment name was accepted")
	}
	if !strings.Contains(err.Error(), EnvNodeRPCAddr) {
		t.Fatalf("error = %q, want canonical environment name %q", err, EnvNodeRPCAddr)
	}
}

func TestCrawlerIdentityUsesCanonicalProductName(t *testing.T) {
	if DefaultWorkerID != "yago-crawler" {
		t.Fatalf("default worker identity = %q", DefaultWorkerID)
	}
	if !strings.HasPrefix(DefaultUserAgent, "yago-crawler/") {
		t.Fatalf("default user agent = %q", DefaultUserAgent)
	}
}
