package yagonode

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func liveCrawlRuntime(t *testing.T) *crawlRuntime {
	t.Helper()
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	runtimeProcess, err := buildRuntimeCrawl(
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

	return runtime
}

func TestCrawlRuntimeAccessorsOnLiveRuntime(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	ctx := context.Background()

	if runtime.orderQueue() == nil {
		t.Fatal("orderQueue nil on live runtime")
	}
	if runtime.dispatcher() == nil {
		t.Fatal("dispatcher nil on live runtime")
	}
	_ = runtime.recrawlSweeper()
	depth, err := runtime.crawlQueueDepth(ctx)
	if err != nil {
		t.Fatalf("crawlQueueDepth: %v", err)
	}
	if depth.Pending != 0 || depth.Leased != 0 {
		t.Fatalf("fresh queue depth = %+v, want empty", depth)
	}
	runtime.observe(metrics.NewCrawlMetrics(prometheus.NewRegistry()))
}

func TestCrawlRuntimeSelectorsResolveLiveRuntime(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	ctx := context.Background()

	if _, ok := crawlRecrawlSweeper(runtime); !ok {
		t.Fatal("crawlRecrawlSweeper should resolve a live runtime")
	}
	if crawlDispatcher(runtime) == nil {
		t.Fatal("crawlDispatcher nil on live runtime")
	}
	if crawlOrderQueue(runtime) == nil {
		t.Fatal("crawlOrderQueue nil on live runtime")
	}
	probe := crawlQueueProbe(runtime)
	if probe == nil {
		t.Fatal("crawlQueueProbe nil on live runtime")
	}
	if _, err := probe(ctx); err != nil {
		t.Fatalf("queue probe: %v", err)
	}
	attachCrawlMetrics(runtime, metrics.NewCrawlMetrics(prometheus.NewRegistry()))
	attachCrawlMetrics(runtime, nil)
}
