package metrics

import "github.com/prometheus/client_golang/prometheus"

type CrawlMetrics struct {
	absorbed   prometheus.Counter
	deferred   prometheus.Counter
	rejected   prometheus.Counter
	duplicates prometheus.Counter
	bytes      prometheus.Counter
	urls       prometheus.Counter
	postings   prometheus.Counter
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
	rejected := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_rejections_total",
		Help: "Malformed crawl ingest batches dropped without absorbing.",
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
	duplicates := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "crawl_ingest_near_duplicates_total",
		Help: "Crawl ingest documents collapsed as near-duplicates of stored pages.",
	})
	registry.MustRegister(absorbed, deferred, rejected, duplicates, bytes, urls, postings)

	return &CrawlMetrics{
		absorbed:   absorbed,
		deferred:   deferred,
		rejected:   rejected,
		duplicates: duplicates,
		bytes:      bytes,
		urls:       urls,
		postings:   postings,
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

func (m *CrawlMetrics) ObserveRejected() {
	m.rejected.Inc()
}

func (m *CrawlMetrics) ObserveDuplicate() {
	m.duplicates.Inc()
}
