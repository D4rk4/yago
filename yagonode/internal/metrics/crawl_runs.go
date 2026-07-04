package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// CrawlRunMetrics tracks crawl runs the node learns about from worker progress
// reports: how many are active right now, how many have reached each terminal
// state, and the cumulative per-outcome page tally of the runs that finished.
type CrawlRunMetrics struct {
	active   prometheus.Gauge
	terminal *prometheus.CounterVec
	outcomes *prometheus.CounterVec
}

// NewCrawlRunMetrics registers the crawl-run collectors and returns a handle the
// node updates as progress reports arrive.
func NewCrawlRunMetrics(registry prometheus.Registerer) *CrawlRunMetrics {
	active := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "crawl_runs_active",
		Help: "Crawl runs the node currently sees as running.",
	})
	terminal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "crawl_runs_total",
		Help: "Crawl runs the node has seen reach a terminal state, by state.",
	}, []string{"state"})
	outcomes := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "crawl_run_outcomes_total",
		Help: "Per-outcome pages tallied across finished crawl runs, by outcome.",
	}, []string{"outcome"})
	registry.MustRegister(active, terminal, outcomes)

	return &CrawlRunMetrics{active: active, terminal: terminal, outcomes: outcomes}
}

// SetActive publishes the number of runs the node currently sees as running.
func (m *CrawlRunMetrics) SetActive(count int) {
	m.active.Set(float64(count))
}

// ObserveTerminal records a run reaching a terminal state exactly once, folding
// its final outcome tally into the cumulative per-outcome counters.
func (m *CrawlRunMetrics) ObserveTerminal(
	state yagocrawlcontract.CrawlRunState,
	tally yagocrawlcontract.CrawlRunTally,
) {
	m.terminal.WithLabelValues(string(state)).Inc()
	m.outcomes.WithLabelValues("fetched").Add(float64(tally.Fetched))
	m.outcomes.WithLabelValues("indexed").Add(float64(tally.Indexed))
	m.outcomes.WithLabelValues("failed").Add(float64(tally.Failed))
	m.outcomes.WithLabelValues("robots_denied").Add(float64(tally.RobotsDenied))
	m.outcomes.WithLabelValues("duplicates").Add(float64(tally.Duplicates))
}
