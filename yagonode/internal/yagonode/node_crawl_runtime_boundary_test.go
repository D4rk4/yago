package yagonode

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func TestLiveCrawlRuntimeUsesDataExchangeWithoutNodeOrderQueue(t *testing.T) {
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	runtimeProcess, err := buildRuntimeCrawl(
		context.Background(),
		crawlConfig{ListenAddr: "127.0.0.1:0"},
		nodeIdentity(testConfig(t)),
		storage,
		storageVault,
	)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	runtime, ok := runtimeProcess.(*crawlRuntime)
	if !ok {
		t.Fatalf("runtime type = %T, want *crawlRuntime", runtimeProcess)
	}
	t.Cleanup(runtime.Close)
	if runtime.exchange == nil || runtime.controlRegistry() == nil || runtime.runRegistry() == nil {
		t.Fatal("crawler data exchange or crawler status registry is unavailable")
	}
	runtime.observe(metrics.NewCrawlMetrics(prometheus.NewRegistry()))
}
