package crawlruns

import (
	"sort"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	defaultThroughputWindow  = time.Minute
	maximumThroughputSamples = 16
	progressFreshness        = 3 * yagocrawlcontract.DefaultWorkerHeartbeatInterval
)

type crawlRunRateSample struct {
	at      time.Time
	fetched uint64
	indexed uint64
}

type workerThroughputAggregate struct {
	worker    WorkerThroughput
	rateKnown bool
}

type WorkerThroughput struct {
	WorkerID              string
	ProcessIdentifier     string
	FetchedPagesPerMinute float64
	IndexedPagesPerMinute float64
	RateKnown             bool
	ObservedAt            time.Time
}

func (r *Registry) recordRateSampleLocked(run Run, at time.Time) {
	if run.State != yagocrawlcontract.CrawlRunRunning {
		delete(r.rateSamples, run.RunID)

		return
	}
	samples := r.rateSamples[run.RunID]
	sample := crawlRunRateSample{at: at, fetched: run.Tally.Fetched, indexed: run.Tally.Indexed}
	if len(samples) > 0 {
		previous := samples[len(samples)-1]
		if sample.fetched < previous.fetched || sample.indexed < previous.indexed {
			samples = nil
		} else if sample.at.Equal(previous.at) {
			samples[len(samples)-1] = sample

			return
		}
	}
	samples = append(samples, sample)
	if len(samples) > maximumThroughputSamples {
		samples = append(
			[]crawlRunRateSample(nil),
			samples[len(samples)-maximumThroughputSamples:]...)
	}
	r.rateSamples[run.RunID] = samples
}

func (r *Registry) Throughput(window time.Duration) []WorkerThroughput {
	if window <= 0 {
		window = defaultThroughputWindow
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	type processIdentity struct {
		workerID          string
		processIdentifier string
	}
	byProcess := make(map[processIdentity]workerThroughputAggregate)
	for runID, run := range r.runs {
		if run.State != yagocrawlcontract.CrawlRunRunning ||
			now.Sub(run.Updated) > progressFreshness || now.Before(run.Updated) {
			continue
		}
		pulse, known := rateForSamples(r.rateSamples[runID], now, window)
		identity := processIdentity{
			workerID:          run.WorkerID,
			processIdentifier: run.ProcessIdentifier,
		}
		aggregate, found := byProcess[identity]
		if !found {
			aggregate.worker = WorkerThroughput{
				WorkerID:          run.WorkerID,
				ProcessIdentifier: run.ProcessIdentifier,
			}
			aggregate.rateKnown = true
		}
		worker := aggregate.worker
		worker.WorkerID = run.WorkerID
		worker.ProcessIdentifier = run.ProcessIdentifier
		worker.ObservedAt = maxTime(worker.ObservedAt, run.Updated)
		if known {
			worker.FetchedPagesPerMinute += pulse.FetchedPagesPerMinute
			worker.IndexedPagesPerMinute += pulse.IndexedPagesPerMinute
		} else {
			aggregate.rateKnown = false
		}
		aggregate.worker = worker
		byProcess[identity] = aggregate
	}

	workers := make([]WorkerThroughput, 0, len(byProcess))
	for _, aggregate := range byProcess {
		worker := aggregate.worker
		worker.RateKnown = aggregate.rateKnown
		workers = append(workers, worker)
	}
	sort.Slice(workers, func(i, j int) bool {
		if workers[i].WorkerID != workers[j].WorkerID {
			return workers[i].WorkerID < workers[j].WorkerID
		}

		return workers[i].ProcessIdentifier < workers[j].ProcessIdentifier
	})

	return workers
}

func rateForSamples(
	samples []crawlRunRateSample,
	now time.Time,
	window time.Duration,
) (WorkerThroughput, bool) {
	if len(samples) < 2 {
		return WorkerThroughput{}, false
	}
	latest := samples[len(samples)-1]
	cutoff := now.Add(-window)
	start := samples[0]
	for _, sample := range samples {
		if sample.at.After(cutoff) {
			break
		}
		start = sample
	}
	elapsed := latest.at.Sub(start.at)
	if elapsed <= 0 {
		return WorkerThroughput{}, false
	}
	minutes := elapsed.Minutes()

	return WorkerThroughput{
		FetchedPagesPerMinute: float64(latest.fetched-start.fetched) / minutes,
		IndexedPagesPerMinute: float64(latest.indexed-start.indexed) / minutes,
		RateKnown:             true,
		ObservedAt:            latest.at,
	}, true
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}

	return left
}
