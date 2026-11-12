package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func (r *crawlRuntime) observeIngestStages(observer crawlresults.IngestStageObserver) {
	r.consumer.ObserveIngestStages(observer)
}

func attachIngestStageMetrics(
	runtime crawlProcess,
	collector *metrics.IngestStageMetrics,
) {
	if collector == nil {
		return
	}
	observed, ok := runtime.(interface {
		observeIngestStages(crawlresults.IngestStageObserver)
	})
	if !ok {
		return
	}

	observed.observeIngestStages(collector)
}
