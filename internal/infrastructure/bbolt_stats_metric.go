package infrastructure

import (
	"expvar"

	bolt "go.etcd.io/bbolt"
)

const MetricBboltStats = "bbolt_stats"

type BboltStatsSource interface {
	Stats() bolt.Stats
}

func (s *BboltStorage) Stats() bolt.Stats {
	if s == nil || s.db == nil {
		return bolt.Stats{}
	}

	return s.db.Stats()
}

func PublishBboltStats(source BboltStatsSource) {
	if expvar.Get(MetricBboltStats) != nil {
		return
	}

	expvar.Publish(MetricBboltStats, expvar.Func(func() any {
		return source.Stats()
	}))
}
