package metrics

import "github.com/prometheus/client_golang/prometheus"

type CrawlMetrics struct {
	absorbed prometheus.Counter
	deferred prometheus.Counter
	bytes    prometheus.Counter
	urls     prometheus.Counter
	postings prometheus.Counter
}

func NewCrawlMetrics(registry prometheus.Registerer) *CrawlMetrics {
	absorbed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_batches_total",
		Help: "Crawl ingest batches absorbed into the index.",
	})
	deferred := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_deferrals_total",
		Help: "Crawl ingest batches deferred back to the queue.",
	})
	bytes := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_content_bytes_total",
		Help: "Extracted content bytes absorbed from crawl ingest.",
	})
	urls := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_urls_total",
		Help: "URL metadata rows absorbed from crawl ingest.",
	})
	postings := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_postings_total",
		Help: "Postings absorbed from crawl ingest.",
	})
	registry.MustRegister(absorbed, deferred, bytes, urls, postings)

	return &CrawlMetrics{
		absorbed: absorbed,
		deferred: deferred,
		bytes:    bytes,
		urls:     urls,
		postings: postings,
	}
}

func (m *CrawlMetrics) ObserveAbsorbed(contentBytes, urls, postings int) {
	m.absorbed.Inc()
	m.bytes.Add(float64(contentBytes))
	m.urls.Add(float64(urls))
	m.postings.Add(float64(postings))
}

func (m *CrawlMetrics) ObserveDeferred() {
	m.deferred.Inc()
}
