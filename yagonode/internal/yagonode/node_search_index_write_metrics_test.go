package yagonode

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

type searchIndexWriteObservedCrawlProcess struct {
	bareCrawlProcess
	observer crawlresults.SearchIndexWriteObserver
}

func (p *searchIndexWriteObservedCrawlProcess) observeSearchIndexWrites(
	observer crawlresults.SearchIndexWriteObserver,
) {
	p.observer = observer
}

func TestAttachSearchIndexWriteMetricsUsesSupportedRuntime(t *testing.T) {
	t.Parallel()

	collector := metrics.NewSearchIndexWriteMetrics(prometheus.NewRegistry())
	runtime := &searchIndexWriteObservedCrawlProcess{}
	attachSearchIndexWriteMetrics(runtime, collector)
	if runtime.observer != collector {
		t.Fatalf("attached observer = %T, want collector", runtime.observer)
	}

	attachSearchIndexWriteMetrics(runtime, nil)
	if runtime.observer != collector {
		t.Fatal("nil collector replaced the attached observer")
	}
	attachSearchIndexWriteMetrics(bareCrawlProcess{}, collector)
}

func TestLiveCrawlRuntimeAcceptsSearchIndexWriteMetrics(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	collector := metrics.NewSearchIndexWriteMetrics(prometheus.NewRegistry())
	runtime.observeSearchIndexWrites(collector)
	attachSearchIndexWriteMetrics(runtime, collector)
}
