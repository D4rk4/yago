package crawlmetrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	labelOutput = "output"
	labelReason = "reason"
	labelKind   = "kind"
)

type CrawlMetrics struct {
	registry          *prometheus.Registry
	ordersReceived    prometheus.Counter
	ordersCompleted   prometheus.Counter
	ordersRedelivered prometheus.Counter
	pagesFetched      prometheus.Counter
	pagesPublished    *prometheus.CounterVec
	pagesDisposed     *prometheus.CounterVec
	refusalsHonored   *prometheus.CounterVec
	publicationWaits  prometheus.Counter
	budgetExhaustions prometheus.Counter
	fetchDurationSecs prometheus.Histogram
}

func New() *CrawlMetrics {
	registry := prometheus.NewRegistry()
	metrics := &CrawlMetrics{
		registry: registry,
		ordersReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacycrawler_orders_received_total",
			Help: "Crawl orders received.",
		}),
		ordersCompleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacycrawler_orders_completed_total",
			Help: "Crawl orders acknowledged after every page reached a terminal outcome.",
		}),
		ordersRedelivered: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacycrawler_orders_redelivered_total",
			Help: "Crawl orders returned for redelivery.",
		}),
		pagesFetched: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacycrawler_pages_fetched_total",
			Help: "Pages fetched.",
		}),
		pagesPublished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "yacycrawler_pages_published_total",
			Help: "Pages published, by output.",
		}, []string{labelOutput}),
		pagesDisposed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "yacycrawler_pages_disposed_total",
			Help: "Pages disposed, by reason.",
		}, []string{labelReason}),
		refusalsHonored: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "yacycrawler_refusals_honored_total",
			Help: "Target refusals honored, by kind.",
		}, []string{labelKind}),
		publicationWaits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacycrawler_publication_waits_total",
			Help: "Waits on transient publication backpressure.",
		}),
		budgetExhaustions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "yacycrawler_budget_exhaustions_total",
			Help: "Runs that reached the page budget with frontier remaining.",
		}),
		fetchDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "yacycrawler_fetch_duration_seconds",
			Help:    "Page fetch duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	registry.MustRegister(
		metrics.ordersReceived,
		metrics.ordersCompleted,
		metrics.ordersRedelivered,
		metrics.pagesFetched,
		metrics.pagesPublished,
		metrics.pagesDisposed,
		metrics.refusalsHonored,
		metrics.publicationWaits,
		metrics.budgetExhaustions,
		metrics.fetchDurationSecs,
	)
	return metrics
}

func (m *CrawlMetrics) OrderReceived()    { m.ordersReceived.Inc() }
func (m *CrawlMetrics) OrderCompleted()   { m.ordersCompleted.Inc() }
func (m *CrawlMetrics) OrderRedelivered() { m.ordersRedelivered.Inc() }
func (m *CrawlMetrics) PageFetched()      { m.pagesFetched.Inc() }

func (m *CrawlMetrics) PagePublished(output string) {
	m.pagesPublished.WithLabelValues(output).Inc()
}

func (m *CrawlMetrics) PageDisposed(reason string) {
	m.pagesDisposed.WithLabelValues(reason).Inc()
}

func (m *CrawlMetrics) RefusalHonored(kind string) {
	m.refusalsHonored.WithLabelValues(kind).Inc()
}

func (m *CrawlMetrics) PublicationWaited() { m.publicationWaits.Inc() }
func (m *CrawlMetrics) BudgetExhausted()   { m.budgetExhaustions.Inc() }

func (m *CrawlMetrics) FetchObserved(elapsed time.Duration) {
	m.fetchDurationSecs.Observe(elapsed.Seconds())
}

func (m *CrawlMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
