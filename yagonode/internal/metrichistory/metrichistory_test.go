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
	sampler               *Sampler
	requests              *prometheus.CounterVec
	durations             prometheus.Histogram
	dht                   prometheus.Counter
	crawl                 prometheus.Gauge
	process               prometheus.Counter
	memory                prometheus.Gauge
	used                  prometheus.Gauge
	quota                 prometheus.Gauge
	hostTotal             uint64
	hostAvailableBytes    uint64
	hostAvailabilityKnown bool
	hostAvailable         bool
	hostReads             int
	now                   time.Time
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
	process := prometheus.NewCounter(prometheus.CounterOpts{Name: "process_cpu_seconds_total"})
	memory := prometheus.NewGauge(prometheus.GaugeOpts{Name: "process_resident_memory_bytes"})
	used := prometheus.NewGauge(prometheus.GaugeOpts{Name: "storage_used_bytes"})
	quota := prometheus.NewGauge(prometheus.GaugeOpts{Name: "storage_quota_bytes"})
	registry.MustRegister(requests, durations, dht, crawl, index, process, memory, used, quota)
	index.Set(3)
	process.Add(1)
	memory.Set(64 << 20)
	used.Set(2 << 30)
	quota.Set(8 << 30)

	h := &harness{
		requests:              requests,
		durations:             durations,
		dht:                   dht,
		crawl:                 crawl,
		process:               process,
		memory:                memory,
		used:                  used,
		quota:                 quota,
		hostTotal:             16 << 30,
		hostAvailableBytes:    4 << 30,
		hostAvailabilityKnown: true,
		hostAvailable:         true,
		now:                   time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC),
	}
	h.sampler = New(registry, capacity, func() (HostMemory, bool) {
		h.hostReads++

		return HostMemory{
			TotalBytes:        h.hostTotal,
			AvailableBytes:    h.hostAvailableBytes,
			AvailableObserved: h.hostAvailabilityKnown,
		}, h.hostAvailable
	})
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

func assertSamplerSeriesEndValues(
	t *testing.T,
	sampler *Sampler,
	wants map[string]float64,
	firstGauges map[string]float64,
) {
	t.Helper()
	for name, want := range wants {
		series := seriesByName(t, sampler, name)
		wantPoints := 1
		if _, first := firstGauges[name]; first {
			wantPoints = 2
		}
		if len(series.Points) != wantPoints ||
			series.Points[len(series.Points)-1].Value != want {
			t.Errorf(
				"%s = %+v, want %d point(s) ending at %v",
				name,
				series.Points,
				wantPoints,
				want,
			)
		}
		if series.Unit == "" {
			t.Errorf("%s must carry a unit", name)
		}
	}
}

func TestSamplerRecordsRatesGaugesAndLatency(t *testing.T) {
	h := newHarness(t, 8)

	h.requests.WithLabelValues("/", "200").Add(10)
	h.sampler.Sample()
	if points := seriesByName(t, h.sampler, SeriesRequests).Points; len(points) != 0 {
		t.Fatalf("the first gathering must only seed the baseline, got %v", points)
	}
	firstGauges := map[string]float64{
		SeriesCrawlQueue:          0,
		SeriesProcessMemory:       64 << 20,
		SeriesHostMemoryTotal:     16 << 30,
		SeriesHostMemoryAvailable: 4 << 30,
		SeriesStorageUse:          2 << 30,
		SeriesStorageCap:          8 << 30,
		SeriesIndexQueue:          3,
	}
	for name, want := range firstGauges {
		points := seriesByName(t, h.sampler, name).Points
		if len(points) != 1 || points[0].Value != want {
			t.Errorf("first %s gauge = %+v, want one point of %v", name, points, want)
		}
	}
	if points := seriesByName(t, h.sampler, SeriesProcessCPU).Points; len(points) != 0 {
		t.Fatalf("first process CPU delta = %v, want unavailable", points)
	}

	h.requests.WithLabelValues("/", "200").Add(20)
	h.requests.WithLabelValues("/", "502").Add(5)
	h.durations.Observe(0.2)
	h.durations.Observe(0.4)
	h.dht.Add(50)
	h.process.Add(5)
	h.crawl.Set(7)
	h.advance()
	h.sampler.Sample()

	wants := map[string]float64{
		SeriesRequests:            2.5, // 25 new requests over 10s
		SeriesErrors:              0.5, // 5 new 5xx over 10s
		SeriesLatency:             300, // (0.2+0.4)/2 seconds in ms
		SeriesDHT:                 5,   // 50 postings over 10s
		SeriesCrawlQueue:          7,
		SeriesProcessCPU:          0.5,
		SeriesProcessMemory:       64 << 20,
		SeriesHostMemoryTotal:     16 << 30,
		SeriesHostMemoryAvailable: 4 << 30,
		SeriesStorageUse:          2 << 30,
		SeriesStorageCap:          8 << 30,
		SeriesIndexQueue:          3,
	}
	if h.hostReads != 2 {
		t.Fatalf("host memory reads = %d, want one per sample", h.hostReads)
	}
	assertSamplerSeriesEndValues(t, h.sampler, wants, firstGauges)

	h.advance()
	h.sampler.Sample()
	latency := seriesByName(t, h.sampler, SeriesLatency)
	if latency.Points[len(latency.Points)-1].Value != 0 {
		t.Errorf(
			"an idle interval must report zero latency, got %v",
			latency.Points[len(latency.Points)-1],
		)
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

func TestSamplerOmitsUnavailableSystemObservations(t *testing.T) {
	h := newHarness(t, 8)
	h.memory.Set(math.NaN())
	h.used.Set(math.NaN())
	h.hostAvailable = false
	h.sampler.Sample()
	h.process.Add(2)
	h.advance()
	h.sampler.Sample()

	for _, name := range []string{SeriesProcessMemory, SeriesStorageUse} {
		if points := seriesByName(t, h.sampler, name).Points; len(points) != 0 {
			t.Fatalf("unknown %s observations = %v", name, points)
		}
	}
	process := seriesByName(t, h.sampler, SeriesProcessCPU).Points
	if len(process) != 1 || process[0].Value != 0.2 {
		t.Fatalf("known process observations = %v", process)
	}
	for _, name := range []string{SeriesHostMemoryTotal, SeriesHostMemoryAvailable} {
		if points := seriesByName(t, h.sampler, name).Points; len(points) != 0 {
			t.Fatalf("unknown %s observations = %v", name, points)
		}
	}
}

func TestSamplerKeepsTotalWhenHostAvailabilityIsInvalid(t *testing.T) {
	h := newHarness(t, 8)
	h.hostAvailabilityKnown = false
	h.sampler.Sample()
	h.advance()
	h.sampler.Sample()

	total := seriesByName(t, h.sampler, SeriesHostMemoryTotal).Points
	if len(total) != 2 || total[len(total)-1].Value != 16<<30 {
		t.Fatalf("host total observations = %v", total)
	}
	available := seriesByName(t, h.sampler, SeriesHostMemoryAvailable).Points
	if len(available) != 0 {
		t.Fatalf("invalid host availability observations = %v", available)
	}

	h.hostTotal = 0
	h.hostAvailabilityKnown = true
	h.advance()
	h.sampler.Sample()
	if total = seriesByName(t, h.sampler, SeriesHostMemoryTotal).Points; len(total) != 2 {
		t.Fatalf("invalid host total observations = %v", total)
	}

	h.hostTotal = 1<<62 + 1
	h.advance()
	h.sampler.Sample()
	if total = seriesByName(t, h.sampler, SeriesHostMemoryTotal).Points; len(total) != 2 {
		t.Fatalf("overflowing host total observations = %v", total)
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

func TestSamplerFirstGatheringPublishesOnlyAvailableGauges(t *testing.T) {
	h := newHarness(t, 8)
	h.crawl.Set(7)
	h.sampler.Sample()

	for _, name := range []string{
		SeriesRequests,
		SeriesErrors,
		SeriesLatency,
		SeriesDHT,
		SeriesProcessCPU,
	} {
		if points := seriesByName(t, h.sampler, name).Points; len(points) != 0 {
			t.Errorf("first %s derived series = %v, want unavailable", name, points)
		}
	}
	for name, want := range map[string]float64{
		SeriesCrawlQueue:          7,
		SeriesProcessMemory:       64 << 20,
		SeriesHostMemoryTotal:     16 << 30,
		SeriesHostMemoryAvailable: 4 << 30,
		SeriesStorageUse:          2 << 30,
		SeriesStorageCap:          8 << 30,
		SeriesIndexQueue:          3,
	} {
		points := seriesByName(t, h.sampler, name).Points
		if len(points) != 1 || points[0].Value != want || points[0].At != h.now {
			t.Errorf("first %s gauge = %+v, want %v at %s", name, points, want, h.now)
		}
	}
}

type failingGatherer struct{}

func (failingGatherer) Gather() ([]*dto.MetricFamily, error) {
	return nil, errors.New("gather failed")
}

type secondGatheringSignal struct {
	calls  int
	second chan struct{}
}

func (g *secondGatheringSignal) Gather() ([]*dto.MetricFamily, error) {
	g.calls++
	if g.calls == 2 {
		close(g.second)
	}

	return nil, nil
}

func TestSamplerToleratesGatherFailuresAndTinyCapacity(t *testing.T) {
	sampler := New(failingGatherer{}, 0, nil)
	sampler.Sample()
	if series := sampler.Series(); len(series) == 0 || len(series[0].Points) != 0 {
		t.Fatalf("a failing gatherer must record nothing: %+v", series)
	}
	if sampler.capacity != 2 {
		t.Fatalf("capacity must clamp to 2, got %d", sampler.capacity)
	}
}

func TestSamplerWithoutHostMemorySource(t *testing.T) {
	sampler := New(prometheus.NewRegistry(), 2, nil)
	sampler.Sample()
	for _, name := range []string{SeriesHostMemoryTotal, SeriesHostMemoryAvailable} {
		if points := seriesByName(t, sampler, name).Points; len(points) != 0 {
			t.Fatalf("host observations without source = %v", points)
		}
	}
}

func TestSamplerRejectsNonFiniteProcessCPU(t *testing.T) {
	notANumber := math.NaN()
	infinity := math.Inf(1)
	valid := 2.5
	current := counters{}
	addProcessCPU(&current, []*dto.Metric{
		{Counter: &dto.Counter{Value: &notANumber}},
		{Counter: &dto.Counter{Value: &infinity}},
		{Counter: &dto.Counter{Value: &valid}},
	})
	if !current.processKnown || current.processCPU != valid {
		t.Fatalf("filtered process CPU = %+v, want %v", current, valid)
	}

	invalid := counters{}
	addProcessCPU(&invalid, []*dto.Metric{
		{Counter: &dto.Counter{Value: &notANumber}},
		{Counter: &dto.Counter{Value: &infinity}},
	})
	if invalid.processKnown || invalid.processCPU != 0 {
		t.Fatalf("non-finite process CPU became known: %+v", invalid)
	}
}

func TestSamplerRunSamplesUntilCancelled(t *testing.T) {
	gatherer := &secondGatheringSignal{second: make(chan struct{})}
	sampler := New(gatherer, 8, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sampler.Run(ctx, time.Millisecond)
	}()

	select {
	case <-gatherer.second:
	case <-time.After(2 * time.Second):
		t.Fatal("Run never sampled on its ticker")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run never stopped after cancel")
	}
}
