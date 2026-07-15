package metrichistory

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type harness struct {
	sampler   *Sampler
	requests  *prometheus.CounterVec
	durations prometheus.Histogram
	dht       prometheus.Counter
	crawl     prometheus.Gauge
	now       time.Time
}

func newHarness(t *testing.T, capacity int) *harness {
	t.Helper()
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "http_requests_total"},
		[]string{"endpoint", "code"},
	)
	durations := prometheus.NewHistogram(
		prometheus.HistogramOpts{Name: "http_request_duration_seconds"},
	)
	dht := prometheus.NewCounter(
		prometheus.CounterOpts{Name: "yacy_dht_outbound_postings_total"},
	)
	crawl := prometheus.NewGauge(prometheus.GaugeOpts{Name: "queue_crawl_depth"})
	index := prometheus.NewGauge(prometheus.GaugeOpts{Name: "queue_index_depth"})
	registry.MustRegister(requests, durations, dht, crawl, index)
	index.Set(3)

	h := &harness{
		sampler:   New(registry, capacity),
		requests:  requests,
		durations: durations,
		dht:       dht,
		crawl:     crawl,
		now:       time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC),
	}
	h.sampler.clock = func() time.Time { return h.now }

	return h
}

func (h *harness) advance() {
	h.now = h.now.Add(10 * time.Second)
}

func seriesByName(t *testing.T, sampler *Sampler, name string) Series {
	t.Helper()
	for _, series := range sampler.Series() {
		if series.Name == name {
			return series
		}
	}
	t.Fatalf("series %q not found", name)

	return Series{}
}

func TestSamplerRecordsRatesGaugesAndLatency(t *testing.T) {
	h := newHarness(t, 8)

	h.requests.WithLabelValues("/", "200").Add(10)
	h.sampler.Sample()
	if points := seriesByName(t, h.sampler, SeriesRequests).Points; len(points) != 0 {
		t.Fatalf("the first gathering must only seed the baseline, got %v", points)
	}

	h.requests.WithLabelValues("/", "200").Add(20)
	h.requests.WithLabelValues("/", "502").Add(5)
	h.durations.Observe(0.2)
	h.durations.Observe(0.4)
	h.dht.Add(50)
	h.crawl.Set(7)
	h.advance()
	h.sampler.Sample()

	wants := map[string]float64{
		SeriesRequests:   2.5, // 25 new requests over 10s
		SeriesErrors:     0.5, // 5 new 5xx over 10s
		SeriesLatency:    300, // (0.2+0.4)/2 seconds in ms
		SeriesDHT:        5,   // 50 postings over 10s
		SeriesCrawlQueue: 7,
		SeriesIndexQueue: 3,
	}
	for name, want := range wants {
		series := seriesByName(t, h.sampler, name)
		if len(series.Points) != 1 || series.Points[0].Value != want {
			t.Errorf("%s = %+v, want one point of %v", name, series.Points, want)
		}
		if series.Unit == "" {
			t.Errorf("%s must carry a unit", name)
		}
	}

	h.advance()
	h.sampler.Sample()
	latency := seriesByName(t, h.sampler, SeriesLatency)
	if latency.Points[1].Value != 0 {
		t.Errorf("an idle interval must report zero latency, got %v", latency.Points[1])
	}
}

func TestSamplerBoundsTheRing(t *testing.T) {
	h := newHarness(t, 3)

	h.sampler.Sample()
	for range 6 {
		h.advance()
		h.sampler.Sample()
	}
	points := seriesByName(t, h.sampler, SeriesRequests).Points
	if len(points) != 3 {
		t.Fatalf("ring length = %d, want the capacity 3", len(points))
	}
	if !points[2].At.After(points[0].At) {
		t.Fatal("points must stay oldest-first")
	}
}

func TestSamplerOmitsUnavailableQueueObservations(t *testing.T) {
	h := newHarness(t, 8)
	h.crawl.Set(math.NaN())
	h.sampler.Sample()
	h.advance()
	h.sampler.Sample()
	if points := seriesByName(t, h.sampler, SeriesCrawlQueue).Points; len(points) != 0 {
		t.Fatalf("unknown queue observations = %v, want none", points)
	}

	h.crawl.Set(6)
	h.advance()
	h.sampler.Sample()
	points := seriesByName(t, h.sampler, SeriesCrawlQueue).Points
	if len(points) != 1 || points[0].Value != 6 {
		t.Fatalf("known queue observations = %v, want one point of 6", points)
	}
}

func TestSamplerSkipsNonPositiveIntervalsAndResets(t *testing.T) {
	h := newHarness(t, 8)

	h.sampler.Sample()
	h.sampler.Sample()
	if points := seriesByName(t, h.sampler, SeriesRequests).Points; len(points) != 0 {
		t.Fatalf("a zero-length interval must not record, got %v", points)
	}

	h.requests.WithLabelValues("/", "200").Add(10)
	h.advance()
	h.sampler.Sample()

	// A counter reset (restart of a subsystem) must clamp to zero, not go negative.
	registry := prometheus.NewRegistry()
	fresh := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "http_requests_total"},
		[]string{"endpoint", "code"},
	)
	registry.MustRegister(fresh)
	h.sampler.gatherer = registry
	h.advance()
	h.sampler.Sample()
	points := seriesByName(t, h.sampler, SeriesRequests).Points
	if got := points[len(points)-1].Value; got != 0 {
		t.Fatalf("a counter reset must record zero, got %v", got)
	}
}

type failingGatherer struct{}

func (failingGatherer) Gather() ([]*dto.MetricFamily, error) {
	return nil, errors.New("gather failed")
}

func TestSamplerToleratesGatherFailuresAndTinyCapacity(t *testing.T) {
	sampler := New(failingGatherer{}, 0)
	sampler.Sample()
	if series := sampler.Series(); len(series) == 0 || len(series[0].Points) != 0 {
		t.Fatalf("a failing gatherer must record nothing: %+v", series)
	}
	if sampler.capacity != 2 {
		t.Fatalf("capacity must clamp to 2, got %d", sampler.capacity)
	}
}

func TestSamplerRunSamplesUntilCancelled(t *testing.T) {
	h := newHarness(t, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.sampler.Run(ctx, time.Millisecond)
	}()

	deadline := time.After(2 * time.Second)
	for {
		h.sampler.mu.Lock()
		seeded := h.sampler.previous.seenGathering
		h.sampler.mu.Unlock()
		if seeded {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Run never sampled")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run never stopped after cancel")
	}
}
