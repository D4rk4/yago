package yagonode

import (
	"context"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
)

// startPerformanceHistorySampler launches the admin Performance-page sampler
// and returns a stop function that cancels it and joins its goroutine.
// bootNode defers the stop so the sampler can never gather the vault-backed
// storage gauges after bootNode returns and run closes the vault.
func startPerformanceHistorySampler(
	ctx context.Context,
	sampler *metrichistory.Sampler,
) func() {
	sampler.Sample()
	ctx, cancel := context.WithCancel(ctx)
	var sampling sync.WaitGroup
	sampling.Add(1)
	go func() {
		defer sampling.Done()
		sampler.RunAfterBaseline(ctx, performanceHistorySampleInterval)
	}()

	return func() {
		cancel()
		sampling.Wait()
	}
}
