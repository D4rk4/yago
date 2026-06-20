package infrastructure

import "expvar"

const MetricEvictionStats = "rwi_eviction_stats"

type EvictionStatsSource interface {
	Sweeps() int64
	URLsEvicted() int64
	PostingsEvicted() int64
}

func PublishEvictionStats(source EvictionStatsSource) {
	if expvar.Get(MetricEvictionStats) != nil {
		return
	}

	expvar.Publish(MetricEvictionStats, expvar.Func(func() any {
		return map[string]int64{
			"sweeps":           source.Sweeps(),
			"urls_evicted":     source.URLsEvicted(),
			"postings_evicted": source.PostingsEvicted(),
		}
	}))
}
