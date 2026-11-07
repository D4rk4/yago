package metrics

import "github.com/prometheus/client_golang/prometheus"

type conditionalRecrawlOutcomes struct {
	conditionalFetchesIssued prometheus.Counter
	notModifiedRecorded      prometheus.Counter
	notModifiedRefused       prometheus.Counter
}

func newConditionalRecrawlOutcomes(registry prometheus.Registerer) conditionalRecrawlOutcomes {
	outcomes := conditionalRecrawlOutcomes{
		conditionalFetchesIssued: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "crawl_recrawl_conditional_fetches_total",
			Help: "Due recrawl orders published with a conditional fetch.",
		}),
		notModifiedRecorded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "crawl_ingest_not_modified_recorded_total",
			Help: "Not-modified observations recorded on the recrawl schedule.",
		}),
		notModifiedRefused: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "crawl_ingest_not_modified_refused_total",
			Help: "Not-modified observations refused as stale, foreign, or without " +
				"a matching stored document, so the crawler fetches the page in full.",
		}),
	}
	registry.MustRegister(
		outcomes.conditionalFetchesIssued,
		outcomes.notModifiedRecorded,
		outcomes.notModifiedRefused,
	)

	return outcomes
}

func (m *CrawlMetrics) ObserveConditionalFetchIssued() {
	m.conditionalRecrawl.conditionalFetchesIssued.Inc()
}

func (m *CrawlMetrics) ObserveNotModifiedRecorded() {
	m.conditionalRecrawl.notModifiedRecorded.Inc()
}

func (m *CrawlMetrics) ObserveNotModifiedRefused() {
	m.conditionalRecrawl.notModifiedRefused.Inc()
}
