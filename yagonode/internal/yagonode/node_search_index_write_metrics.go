package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func (r *crawlRuntime) observeSearchIndexWrites(
	observer crawlresults.SearchIndexWriteObserver,
) {
	r.consumer.ObserveSearchIndexWrites(observer)
}

func attachSearchIndexWriteMetrics(
	runtime crawlProcess,
	collector *metrics.SearchIndexWriteMetrics,
) {
	if collector == nil {
		return
	}
	observed, ok := runtime.(interface {
		observeSearchIndexWrites(crawlresults.SearchIndexWriteObserver)
	})
	if !ok {
		return
	}

	observed.observeSearchIndexWrites(collector)
}
