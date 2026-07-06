// Package crawlermetrics collects crawler activity as Prometheus series so an
// operator can watch fetch volume, failures, bytes downloaded, robots denials,
// active jobs, and published ingest batches.
package crawlermetrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry      *prometheus.Registry
	jobsActive    prometheus.Gauge
	fetches       prometheus.Counter
	fetchFailures prometheus.Counter
	hostBackoffs  prometheus.Counter
	bytes         prometheus.Counter
	robotsDenied  prometheus.Counter
	ingestBatches prometheus.Counter
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	jobsActive := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "yacy_crawler_jobs_active",
		Help: "Crawl jobs currently being fetched and processed.",
	})
	fetches := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_fetches_total",
		Help: "Page fetch attempts.",
	})
	fetchFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_fetch_failures_total",
		Help: "Page fetch attempts that failed for reasons other than a policy rejection.",
	})
	bytes := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_bytes_total",
		Help: "Bytes read from successfully fetched page bodies.",
	})
	robotsDenied := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_robots_denied_total",
		Help: "URLs denied by robots.txt before fetching.",
	})
	ingestBatches := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_ingest_batches_total",
		Help: "Ingest batches accepted by the node.",
	})
	hostBackoffs := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_crawler_host_backoffs_total",
		Help: "Hosts backed off after a 429/503 or Retry-After throttle signal.",
	})
	registry.MustRegister(
		jobsActive, fetches, fetchFailures, bytes, robotsDenied, ingestBatches, hostBackoffs,
	)

	return &Metrics{
		registry:      registry,
		jobsActive:    jobsActive,
		fetches:       fetches,
		fetchFailures: fetchFailures,
		bytes:         bytes,
		robotsDenied:  robotsDenied,
		ingestBatches: ingestBatches,
		hostBackoffs:  hostBackoffs,
	}
}

// ObserveHostBackoff counts a host backed off after a server throttle signal.
func (m *Metrics) ObserveHostBackoff() {
	m.hostBackoffs.Inc()
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) JobStarted() { m.jobsActive.Inc() }

func (m *Metrics) JobFinished() { m.jobsActive.Dec() }

func (m *Metrics) FetchAttempted() { m.fetches.Inc() }

func (m *Metrics) FetchSucceeded(count int) { m.bytes.Add(float64(count)) }

func (m *Metrics) FetchFailed() { m.fetchFailures.Inc() }

func (m *Metrics) RobotsDenied() { m.robotsDenied.Inc() }

func (m *Metrics) IngestPublished() { m.ingestBatches.Inc() }
