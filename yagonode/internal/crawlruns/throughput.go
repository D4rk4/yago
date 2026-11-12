package crawlruns

import (
	"sort"
	"time"
)

const (
	defaultThroughputWindow  = time.Minute
	maximumThroughputSamples = 64
	throughputSampleInterval = time.Second
)

type crawlRunRateSample struct {
	at      time.Time
	fetched uint64
	indexed uint64
}

type crawlerProcessIdentity struct {
	workerID          string
	processIdentifier string
}

type processThroughputState struct {
	fetched uint64
	indexed uint64
	samples []crawlRunRateSample
}

type WorkerThroughput struct {
	WorkerID              string
	ProcessIdentifier     string
	FetchedPagesPerMinute float64
	IndexedPagesPerMinute float64
	RateKnown             bool
	ObservedAt            time.Time
}

func (r *Registry) recordProcessThroughputLocked(
	previous Run,
	existed bool,
	run Run,
	at time.Time,
) {
	identity := crawlerProcessIdentity{
		workerID:          run.WorkerID,
		processIdentifier: run.ProcessIdentifier,
	}
	state := r.processRates[identity]
	if existed && sameCrawlerProcess(previous, run) {
		if run.Tally.Fetched < previous.Tally.Fetched ||
			run.Tally.Indexed < previous.Tally.Indexed {
			state.samples = nil
		} else {
			state.fetched += run.Tally.Fetched - previous.Tally.Fetched
			state.indexed += run.Tally.Indexed - previous.Tally.Indexed
		}
	} else {
		state.fetched += run.Tally.Fetched
		state.indexed += run.Tally.Indexed
	}

	sample := crawlRunRateSample{at: at, fetched: state.fetched, indexed: state.indexed}
	if len(state.samples) > 0 {
		previousSample := state.samples[len(state.samples)-1]
		switch {
		case sample.at.Before(previousSample.at):
			state.samples = nil
		case sample.at.Truncate(throughputSampleInterval).
			Equal(previousSample.at.Truncate(throughputSampleInterval)):
			state.samples[len(state.samples)-1] = sample
			r.processRates[identity] = state

			return
		}
	}
	state.samples = append(state.samples, sample)
	if len(state.samples) > maximumThroughputSamples {
		state.samples = append(
			[]crawlRunRateSample(nil),
			state.samples[len(state.samples)-maximumThroughputSamples:]...)
	}
	r.processRates[identity] = state
}

func sameCrawlerProcess(left, right Run) bool {
	return left.WorkerID == right.WorkerID &&
		left.ProcessIdentifier == right.ProcessIdentifier
}

func (r *Registry) Throughput(window time.Duration) []WorkerThroughput {
	if window <= 0 {
		window = defaultThroughputWindow
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	workers := make([]WorkerThroughput, 0, len(r.processRates))
	retention := max(defaultThroughputWindow, window)
	for identity, state := range r.processRates {
		latest := state.samples[len(state.samples)-1]
		if now.Before(latest.at) {
			continue
		}
		age := now.Sub(latest.at)
		if age > retention {
			delete(r.processRates, identity)

			continue
		}
		if age > window {
			continue
		}
		pulse, known := rateForSamples(state.samples, now, window)
		workers = append(workers, WorkerThroughput{
			WorkerID:              identity.workerID,
			ProcessIdentifier:     identity.processIdentifier,
			FetchedPagesPerMinute: pulse.FetchedPagesPerMinute,
			IndexedPagesPerMinute: pulse.IndexedPagesPerMinute,
			RateKnown:             known,
			ObservedAt:            latest.at,
		})
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
	minutes := elapsed.Minutes()

	return WorkerThroughput{
		FetchedPagesPerMinute: float64(latest.fetched-start.fetched) / minutes,
		IndexedPagesPerMinute: float64(latest.indexed-start.indexed) / minutes,
		RateKnown:             true,
		ObservedAt:            latest.at,
	}, true
}
