// Package metrichistory samples a curated set of the node's Prometheus metrics
// into a bounded in-memory ring, so the admin Performance page can render a
// short operational history (request throughput, error rate, latency, DHT
// transfer rate, queue depths) without an external metrics stack. The ring is
// deliberately small and volatile: durable history belongs to a real
// Prometheus server scraping /metrics.
package metrichistory

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// SeriesRequests through SeriesIndexQueue name the sampled series in the order
// the Performance page presents them.
const (
	SeriesRequests            = "HTTP requests"
	SeriesErrors              = "HTTP 5xx responses"
	SeriesLatency             = "Request latency"
	SeriesDHT                 = "DHT postings sent"
	SeriesCrawlQueue          = "Crawl queue depth"
	SeriesProcessCPU          = "Process CPU"
	SeriesProcessMemory       = "Process memory"
	SeriesHostMemoryTotal     = "Host memory total"
	SeriesHostMemoryAvailable = "Host memory available"
	SeriesStorageUse          = "Storage used"
	SeriesStorageCap          = "Storage quota"
	SeriesIndexQueue          = "Index queue depth"
)

type HostMemory struct {
	TotalBytes        uint64
	AvailableBytes    uint64
	AvailableObserved bool
}

// Point is one sampled value.
type Point struct {
	At    time.Time
	Value float64
}

// Series is one sampled metric's recent history, oldest first.
type Series struct {
	Name   string
	Unit   string
	Points []Point
}

type sample struct {
	at     time.Time
	values map[string]float64
}

// Sampler owns the ring. Sample is driven by Run's ticker; Series serves the
// admin page.
type Sampler struct {
	gatherer   prometheus.Gatherer
	capacity   int
	clock      func() time.Time
	hostMemory func() (HostMemory, bool)

	mu       sync.Mutex
	ring     []sample
	previous counters
}

type counters struct {
	at            time.Time
	requests      float64
	errors        float64
	latencySum    float64
	latencyCount  float64
	dhtPostings   float64
	processCPU    float64
	processKnown  bool
	seenGathering bool
}

// New builds a sampler over the node's shared metrics registry, keeping at
// most capacity points per series.
func New(
	gatherer prometheus.Gatherer,
	capacity int,
	hostMemory func() (HostMemory, bool),
) *Sampler {
	if capacity < 2 {
		capacity = 2
	}

	return &Sampler{
		gatherer:   gatherer,
		capacity:   capacity,
		clock:      time.Now,
		hostMemory: hostMemory,
	}
}

// Run samples on the interval until the context ends.
func (s *Sampler) Run(ctx context.Context, interval time.Duration) {
	s.Sample()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Sample()
		}
	}
}

// Sample gathers the registry once and appends a point per series. Counter
// series record the rate since the previous sample, so the first gathering
// only seeds the baseline.
func (s *Sampler) Sample() {
	families, err := s.gatherer.Gather()
	if err != nil {
		return
	}
	now := s.clock()
	current := readCounters(families, now)
	gauges := readGauges(families)
	for name, value := range readHostMemory(s.hostMemory) {
		gauges[name] = value
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.previous
	s.previous = current
	if !previous.seenGathering {
		return
	}
	elapsed := current.at.Sub(previous.at).Seconds()
	if elapsed <= 0 {
		return
	}
	values := map[string]float64{
		SeriesRequests: rate(current.requests, previous.requests, elapsed),
		SeriesErrors:   rate(current.errors, previous.errors, elapsed),
		SeriesLatency:  latencyMillis(current, previous),
		SeriesDHT:      rate(current.dhtPostings, previous.dhtPostings, elapsed),
	}
	if current.processKnown && previous.processKnown {
		values[SeriesProcessCPU] = rate(current.processCPU, previous.processCPU, elapsed)
	}
	for name, value := range gauges {
		values[name] = value
	}
	s.ring = append(s.ring, sample{at: now, values: values})
	if len(s.ring) > s.capacity {
		s.ring = s.ring[len(s.ring)-s.capacity:]
	}
}

// Series returns the sampled history, oldest point first.
func (s *Sampler) Series() []Series {
	s.mu.Lock()
	defer s.mu.Unlock()
	specs := []struct{ name, unit string }{
		{SeriesRequests, "req/s"},
		{SeriesErrors, "err/s"},
		{SeriesLatency, "ms"},
		{SeriesDHT, "postings/s"},
		{SeriesCrawlQueue, "entries"},
		{SeriesProcessCPU, "cores"},
		{SeriesProcessMemory, "bytes"},
		{SeriesHostMemoryTotal, "bytes"},
		{SeriesHostMemoryAvailable, "bytes"},
		{SeriesStorageUse, "bytes"},
		{SeriesStorageCap, "bytes"},
		{SeriesIndexQueue, "entries"},
	}
	out := make([]Series, 0, len(specs))
	for _, spec := range specs {
		points := make([]Point, 0, len(s.ring))
		for _, entry := range s.ring {
			if value, known := entry.values[spec.name]; known {
				points = append(points, Point{At: entry.at, Value: value})
			}
		}
		out = append(out, Series{Name: spec.name, Unit: spec.unit, Points: points})
	}

	return out
}

func rate(current, previous, elapsed float64) float64 {
	delta := current - previous
	if delta < 0 {
		delta = 0
	}

	return delta / elapsed
}

// latencyMillis averages the request-duration histogram over the interval; an
// interval with no requests reports zero rather than a stale lifetime average.
func latencyMillis(current, previous counters) float64 {
	count := current.latencyCount - previous.latencyCount
	sum := current.latencySum - previous.latencySum
	if count <= 0 || sum < 0 {
		return 0
	}

	return math.Round(sum/count*100_000) / 100
}

func readCounters(families []*dto.MetricFamily, now time.Time) counters {
	out := counters{at: now, seenGathering: true}
	for _, family := range families {
		addCounterFamily(&out, family)
	}

	return out
}

func addCounterFamily(out *counters, family *dto.MetricFamily) {
	switch family.GetName() {
	case "http_requests_total":
		addRequestCounters(out, family.GetMetric())
	case "http_request_duration_seconds":
		addLatencyCounters(out, family.GetMetric())
	case "yacy_dht_outbound_postings_total":
		addDHTPostings(out, family.GetMetric())
	case "process_cpu_seconds_total":
		addProcessCPU(out, family.GetMetric())
	}
}

func addRequestCounters(out *counters, metrics []*dto.Metric) {
	for _, metric := range metrics {
		value := metric.GetCounter().GetValue()
		out.requests += value
		if serverErrorCode(metric) {
			out.errors += value
		}
	}
}

func addLatencyCounters(out *counters, metrics []*dto.Metric) {
	for _, metric := range metrics {
		out.latencySum += metric.GetHistogram().GetSampleSum()
		out.latencyCount += float64(metric.GetHistogram().GetSampleCount())
	}
}

func addDHTPostings(out *counters, metrics []*dto.Metric) {
	for _, metric := range metrics {
		out.dhtPostings += metric.GetCounter().GetValue()
	}
}

func addProcessCPU(out *counters, metrics []*dto.Metric) {
	for _, metric := range metrics {
		value := metric.GetCounter().GetValue()
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		out.processCPU += value
		out.processKnown = true
	}
}

func readGauges(families []*dto.MetricFamily) map[string]float64 {
	out := map[string]float64{}
	for _, family := range families {
		var name string
		switch family.GetName() {
		case "queue_crawl_depth":
			name = SeriesCrawlQueue
		case "queue_index_depth":
			name = SeriesIndexQueue
		case "process_resident_memory_bytes":
			name = SeriesProcessMemory
		case "storage_used_bytes":
			name = SeriesStorageUse
		case "storage_quota_bytes":
			name = SeriesStorageCap
		default:
			continue
		}
		for _, metric := range family.GetMetric() {
			value := metric.GetGauge().GetValue()
			if math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			out[name] += value
		}
	}

	return out
}

func readHostMemory(source func() (HostMemory, bool)) map[string]float64 {
	out := map[string]float64{}
	if source == nil {
		return out
	}
	observation, available := source()
	if !available || observation.TotalBytes == 0 || observation.TotalBytes > 1<<62 {
		return out
	}
	out[SeriesHostMemoryTotal] = float64(observation.TotalBytes)
	if observation.AvailableObserved && observation.AvailableBytes <= 1<<62 {
		out[SeriesHostMemoryAvailable] = float64(observation.AvailableBytes)
	}

	return out
}

func serverErrorCode(metric *dto.Metric) bool {
	for _, label := range metric.GetLabel() {
		if label.GetName() == "code" && strings.HasPrefix(label.GetValue(), "5") {
			return true
		}
	}

	return false
}
