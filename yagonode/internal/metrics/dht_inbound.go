package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type DHTInboundRWIResult struct {
	ReceivedPostings int
	RejectedPostings int
	UnknownURLs      int
	Duration         time.Duration
}

type DHTInboundURLResult struct {
	ReceivedRows   int
	RejectedRows   int
	ReconciledRows int
}

type DHTInboundMetrics struct {
	rwiReceived   prometheus.Counter
	rwiRejected   prometheus.Counter
	rwiUnknownURL prometheus.Counter
	rwiDuration   prometheus.Histogram
	urlReceived   prometheus.Counter
	urlRejected   prometheus.Counter
	urlReconciled prometheus.Counter
}

func NewDHTInboundMetrics(registry prometheus.Registerer) *DHTInboundMetrics {
	rwiReceived := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_rwi_received_postings_total",
		Help: "Inbound RWI postings accepted by transferRWI.",
	})
	rwiRejected := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_rwi_rejected_postings_total",
		Help: "Inbound RWI postings rejected by transferRWI intake.",
	})
	rwiUnknownURL := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_rwi_unknown_url_total",
		Help: "URL metadata hashes requested after inbound transferRWI intake.",
	})
	rwiDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "yacy_rwi_ingest_duration_seconds",
		Help:    "Duration of inbound transferRWI intake.",
		Buckets: prometheus.DefBuckets,
	})
	urlReceived := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_url_metadata_received_total",
		Help: "Inbound URL metadata rows accepted by transferURL.",
	})
	urlRejected := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_url_metadata_rejected_total",
		Help: "Inbound URL metadata rows rejected by transferURL intake.",
	})
	urlReconciled := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_url_metadata_reconciled_total",
		Help: "Inbound URL metadata rows that resolved locally referenced RWI URLs.",
	})
	registry.MustRegister(
		rwiReceived,
		rwiRejected,
		rwiUnknownURL,
		rwiDuration,
		urlReceived,
		urlRejected,
		urlReconciled,
	)

	return &DHTInboundMetrics{
		rwiReceived:   rwiReceived,
		rwiRejected:   rwiRejected,
		rwiUnknownURL: rwiUnknownURL,
		rwiDuration:   rwiDuration,
		urlReceived:   urlReceived,
		urlRejected:   urlRejected,
		urlReconciled: urlReconciled,
	}
}

func (m *DHTInboundMetrics) ObserveRWI(result DHTInboundRWIResult) {
	m.rwiReceived.Add(float64(result.ReceivedPostings))
	m.rwiRejected.Add(float64(result.RejectedPostings))
	m.rwiUnknownURL.Add(float64(result.UnknownURLs))
	m.rwiDuration.Observe(result.Duration.Seconds())
}

func (m *DHTInboundMetrics) ObserveURL(result DHTInboundURLResult) {
	m.urlReceived.Add(float64(result.ReceivedRows))
	m.urlRejected.Add(float64(result.RejectedRows))
	m.urlReconciled.Add(float64(result.ReconciledRows))
}
