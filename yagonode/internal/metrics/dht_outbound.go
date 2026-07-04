package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

type DHTOutboundMetrics struct {
	batches    prometheus.Counter
	postings   prometheus.Counter
	failures   prometheus.Counter
	unknownURL prometheus.Counter
}

func NewDHTOutboundMetrics(registry prometheus.Registerer) *DHTOutboundMetrics {
	batches := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_dht_outbound_batches_total",
		Help: "Outbound DHT chunks accepted by remote peers.",
	})
	postings := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_dht_outbound_postings_total",
		Help: "Outbound DHT postings accepted by remote peers.",
	})
	failures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_dht_outbound_failures_total",
		Help: "Outbound DHT distribution cycles that ended in capacity, transfer, or protocol failure.",
	})
	unknownURL := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "yacy_dht_outbound_unknown_url_total",
		Help: "URL metadata rows requested by remote peers during outbound DHT transfer.",
	})
	registry.MustRegister(batches, postings, failures, unknownURL)

	return &DHTOutboundMetrics{
		batches:    batches,
		postings:   postings,
		failures:   failures,
		unknownURL: unknownURL,
	}
}

func (d *DHTOutboundMetrics) Observe(receipt dhtexchange.DistributionReceipt) {
	if len(receipt.Handoff.RemoteUnknownURL) > 0 {
		d.unknownURL.Add(float64(len(receipt.Handoff.RemoteUnknownURL)))
	}

	switch receipt.State {
	case dhtexchange.DistributionSent:
		d.batches.Inc()
		d.postings.Add(float64(receipt.PostingCount))
	case dhtexchange.DistributionCapacityFailed,
		dhtexchange.DistributionHandoffFailed,
		dhtexchange.DistributionHandoffRejected:
		d.failures.Inc()
	}
}
