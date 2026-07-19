// Package crawlermetrics collects crawler activity as Prometheus series so an
// operator can watch fetch volume, failures, bytes downloaded, robots denials,
// active jobs, and published ingest batches.
package crawlermetrics

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/D4rk4/yago/yago-crawler/internal/browserpool"
)

type Metrics struct {
	registry                        *prometheus.Registry
	jobsActive                      prometheus.Gauge
	fetches                         prometheus.Counter
	fetchFailures                   prometheus.Counter
	parseFailures                   prometheus.Counter
	hostBackoffs                    prometheus.Counter
	bytes                           prometheus.Counter
	robotsDenied                    prometheus.Counter
	ingestBatches                   prometheus.Counter
	browserSlotAcquisitionDeadlines prometheus.Counter
	browserSlotWait                 prometheus.Histogram
	browserSessions                 *prometheus.GaugeVec
	browserFailures                 *prometheus.CounterVec
	activeFetchWorkerJobs           atomic.Uint32
}

func New() *Metrics {
	metrics := newCrawlerActivityMetrics()
	for _, state := range browserpool.SessionStates() {
		metrics.browserSessions.WithLabelValues(string(state)).Set(0)
	}
	for _, reason := range browserpool.FailureReasons() {
		metrics.browserFailures.WithLabelValues(string(reason)).Add(0)
	}
	metrics.registry = prometheus.NewRegistry()
	metrics.registry.MustRegister(metrics.prometheusCollectors()...)

	return metrics
}

func newCrawlerActivityMetrics() *Metrics {
	return &Metrics{
		jobsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "yacy_crawler_jobs_active",
			Help: "Crawl jobs currently being fetched and processed.",
		}),
		fetches: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_fetches_total",
			Help: "Page fetch attempts.",
		}),
		fetchFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_fetch_failures_total",
			Help: "Page fetch attempts that failed for reasons other than a policy rejection.",
		}),
		parseFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_parse_failures_total",
			Help: "Fetched page bodies that no enabled parser could decode.",
		}),
		bytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_bytes_total",
			Help: "Bytes read from successfully fetched page bodies.",
		}),
		robotsDenied: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_robots_denied_total",
			Help: "URLs denied by robots.txt before fetching.",
		}),
		ingestBatches: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_ingest_batches_total",
			Help: "Ingest batches accepted by the node.",
		}),
		hostBackoffs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_host_backoffs_total",
			Help: "Hosts backed off after a 429/503 or Retry-After throttle signal.",
		}),
		browserSlotAcquisitionDeadlines: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacy_crawler_browser_slot_acquisition_deadlines_total",
			Help: "Firefox browser slot acquisition waits ended by a context deadline.",
		}),
		browserSlotWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "yacy_crawler_browser_slot_acquisition_seconds",
			Help:    "Time spent waiting for a Firefox browser session.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),
		browserSessions: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "yacy_crawler_browser_sessions",
			Help: "Firefox browser sessions by bounded pool state.",
		}, []string{"state"}),
		browserFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "yacy_crawler_browser_failures_total",
			Help: "Firefox browser failures by bounded operational reason.",
		}, []string{"reason"}),
	}
}

func (m *Metrics) prometheusCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.jobsActive,
		m.fetches,
		m.fetchFailures,
		m.parseFailures,
		m.bytes,
		m.robotsDenied,
		m.ingestBatches,
		m.hostBackoffs,
		m.browserSlotAcquisitionDeadlines,
		m.browserSlotWait,
		m.browserSessions,
		m.browserFailures,
	}
}

// ObserveHostBackoff counts a host backed off after a server throttle signal.
func (m *Metrics) ObserveHostBackoff() {
	m.hostBackoffs.Inc()
}

func (m *Metrics) ObserveBrowserSlotAcquisitionDeadline() {
	m.browserSlotAcquisitionDeadlines.Inc()
}

func (m *Metrics) ObserveBrowserSlotWait(elapsed time.Duration) {
	m.browserSlotWait.Observe(elapsed.Seconds())
}

func (m *Metrics) ObserveBrowserPoolState(state browserpool.State) {
	m.browserSessions.WithLabelValues(string(browserpool.SessionReady)).Set(float64(state.Ready))
	m.browserSessions.WithLabelValues(string(browserpool.SessionBusy)).Set(float64(state.Busy))
	m.browserSessions.WithLabelValues(string(browserpool.SessionCooling)).
		Set(float64(state.Cooling))
}

func (m *Metrics) ObserveBrowserFailure(reason browserpool.FailureReason) {
	if !reason.Recognized() {
		return
	}
	if reason == browserpool.FailureSlotDeadline {
		m.ObserveBrowserSlotAcquisitionDeadline()
	}
	m.browserFailures.WithLabelValues(string(reason)).Inc()
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) JobStarted() {
	m.activeFetchWorkerJobs.Add(1)
	m.jobsActive.Inc()
}

func (m *Metrics) JobFinished() {
	m.jobsActive.Dec()
	m.activeFetchWorkerJobs.Add(^uint32(0))
}

func (m *Metrics) ActiveFetchWorkerJobs() uint32 {
	return m.activeFetchWorkerJobs.Load()
}

func (m *Metrics) FetchAttempted() { m.fetches.Inc() }

func (m *Metrics) FetchSucceeded(count int) { m.bytes.Add(float64(count)) }

func (m *Metrics) FetchFailed() { m.fetchFailures.Inc() }

func (m *Metrics) ParseFailed() { m.parseFailures.Inc() }

func (m *Metrics) RobotsDenied() { m.robotsDenied.Inc() }

func (m *Metrics) IngestPublished() { m.ingestBatches.Inc() }
