package bootstrap

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type seedlistFetchResult struct {
	position int
	seeds    []yagomodel.Seed
	err      error
}

type seedlistRefresh struct {
	parent    context.Context
	operation context.Context
	cancel    context.CancelFunc
	urls      []string
	observer  SeedImportObserver
	current   time.Time
	results   <-chan seedlistFetchResult
}

type seedlistWorkerPool struct {
	operation context.Context
	fetcher   seedlistFetcher
	urls      []string
	jobs      <-chan int
	results   chan<- seedlistFetchResult
}

func (s *seedlists) startRefresh(parent context.Context) seedlistRefresh {
	operation, cancel := context.WithTimeout(parent, s.refreshTimeout())
	current := time.Now().UTC()
	if s.now != nil {
		current = s.now().UTC()
	}

	return seedlistRefresh{
		parent:    parent,
		operation: operation,
		cancel:    cancel,
		urls:      s.urls,
		observer:  s.observer,
		current:   current,
		results: startSeedlistWorkers(
			operation,
			s.fetcher,
			s.urls,
			s.workerCount(),
		),
	}
}

func (s *seedlists) refreshTimeout() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}

	return seedlistAggregateTimeout
}

func (s *seedlists) workerCount() int {
	if s.concurrency > 0 {
		return min(s.concurrency, len(s.urls))
	}

	return min(seedlistFetchConcurrency, len(s.urls))
}

func startSeedlistWorkers(
	operation context.Context,
	fetcher seedlistFetcher,
	urls []string,
	workers int,
) <-chan seedlistFetchResult {
	jobs := make(chan int)
	results := make(chan seedlistFetchResult)
	pool := seedlistWorkerPool{
		operation: operation,
		fetcher:   fetcher,
		urls:      urls,
		jobs:      jobs,
		results:   results,
	}
	var group sync.WaitGroup
	for range workers {
		group.Go(pool.fetch)
	}
	go queueSeedlistJobs(operation, len(urls), jobs)
	go closeSeedlistResults(&group, results)

	return results
}

func (p seedlistWorkerPool) fetch() {
	for position := range p.jobs {
		seeds, err := p.fetcher.Fetch(p.operation, p.urls[position])
		select {
		case p.results <- seedlistFetchResult{position: position, seeds: seeds, err: err}:
		case <-p.operation.Done():
			return
		}
	}
}

func queueSeedlistJobs(operation context.Context, total int, jobs chan<- int) {
	defer close(jobs)
	for position := range total {
		select {
		case jobs <- position:
		case <-operation.Done():
			return
		}
	}
}

func closeSeedlistResults(group *sync.WaitGroup, results chan seedlistFetchResult) {
	group.Wait()
	close(results)
}

func (r seedlistRefresh) collect() []yagomodel.Seed {
	aggregate := newSeedAggregate()
	completed := 0
	for {
		select {
		case fetched, open := <-r.results:
			if !open {
				return r.result(aggregate)
			}
			completed++
			r.admit(aggregate, fetched)
		case <-r.operation.Done():
			if context.Cause(r.parent) == nil {
				slog.WarnContext(
					r.parent,
					seedlistAggregateExpiredMessage,
					slog.Int("completedSources", completed),
					slog.Int("configuredSources", len(r.urls)),
				)
			}

			return r.result(aggregate)
		}
	}
}

func (r seedlistRefresh) admit(aggregate *seedAggregate, fetched seedlistFetchResult) {
	if fetched.err != nil {
		slog.WarnContext(
			r.parent,
			seedlistFetchFailedMessage,
			slog.String("url", r.urls[fetched.position]),
			slog.Any("error", fetched.err),
		)

		return
	}
	if r.observer != nil {
		r.observer.ObserveSeedlistImport(len(fetched.seeds))
	}
	for position, seed := range fetched.seeds {
		aggregate.admit(seed, fetched.position, position, r.current)
	}
}

func (r seedlistRefresh) result(aggregate *seedAggregate) []yagomodel.Seed {
	if context.Cause(r.parent) != nil {
		return nil
	}

	return aggregate.result()
}
