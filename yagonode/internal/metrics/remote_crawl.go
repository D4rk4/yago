package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

type RemoteCrawlMetrics struct {
	decisions *prometheus.CounterVec
}

func NewRemoteCrawlMetrics(registry prometheus.Registerer) *RemoteCrawlMetrics {
	decisions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "remote_crawl_decisions_total",
		Help: "Remote crawl delegation decisions by action and outcome.",
	}, []string{"action", "outcome"})
	registry.MustRegister(decisions)

	return &RemoteCrawlMetrics{decisions: decisions}
}

func (m *RemoteCrawlMetrics) ObserveRemoteCrawl(observation remotecrawl.Observation) {
	increment := observation.Count
	if increment < 1 {
		increment = 1
	}
	m.decisions.WithLabelValues(observation.Action, observation.Outcome).Add(float64(increment))
}
