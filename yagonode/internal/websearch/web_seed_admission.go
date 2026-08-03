package websearch

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	webSeedConcurrentWrites = 2
	webSeedPendingPerWorker = 64
	webSeedWriteTimeout     = 10 * time.Second
	// webSeedResultSetsQueued is how many complete fallback answers the warming
	// queue must be able to hold. One answer surfaces up to MaxResults URLs and
	// submits them together, so a capacity fixed independently of MaxResults
	// silently dropped seeds once a few queries overlapped. The queue is sized
	// to the larger of its own default and this many result sets.
	webSeedResultSetsQueued = 4
)

var (
	webSeedAdmissionOnce  sync.Once
	webSeedAdmissionValue *webSeedAdmission
	webSeedAdmissionSize  atomic.Int64
)

// SizeSeedAdmission raises the process-wide warming queue so it can hold
// webSeedResultSetsQueued complete answers of maxResults URLs each. It has no
// effect once the queue exists, so callers size it before the first search;
// the queue is process-wide because its workers are, and the largest request
// wins.
func SizeSeedAdmission(maxResults int) {
	wanted := int64(maxResults * webSeedResultSetsQueued)
	for {
		current := webSeedAdmissionSize.Load()
		if wanted <= current || webSeedAdmissionSize.CompareAndSwap(current, wanted) {
			return
		}
	}
}

func webSeedProcessAdmission() *webSeedAdmission {
	webSeedAdmissionOnce.Do(func() {
		pending := max(
			webSeedConcurrentWrites*webSeedPendingPerWorker,
			int(webSeedAdmissionSize.Load()),
		)
		webSeedAdmissionValue = newWebSeedAdmission(webSeedConcurrentWrites, pending)
	})

	return webSeedAdmissionValue
}

type webSeedAdmission struct {
	mutex    sync.Mutex
	pending  chan webSeedWork
	admitted map[string]struct{}
}

type webSeedWork struct {
	key string
	run func(context.Context)
}

func newWebSeedAdmission(workers, pending int) *webSeedAdmission {
	admission := &webSeedAdmission{
		pending:  make(chan webSeedWork, pending),
		admitted: make(map[string]struct{}, pending),
	}
	for range workers {
		go admission.run()
	}

	return admission
}

func (a *webSeedAdmission) try(
	key string,
	_ context.Context,
	work func(context.Context),
) bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if _, duplicate := a.admitted[key]; duplicate {
		return true
	}
	select {
	case a.pending <- webSeedWork{key: key, run: work}:
		a.admitted[key] = struct{}{}

		return true
	default:
		return false
	}
}

func (a *webSeedAdmission) run() {
	for work := range a.pending {
		a.execute(work)
	}
}

func (a *webSeedAdmission) execute(work webSeedWork) {
	defer func() {
		if recover() != nil {
			slog.ErrorContext(context.Background(), msgWebSeedPanicked)
		}
		a.mutex.Lock()
		delete(a.admitted, work.key)
		a.mutex.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), webSeedWriteTimeout)
	defer cancel()
	work.run(ctx)
}

func (s *FallbackSearcher) seedWebResults(ctx context.Context, results []Result) {
	urls := resultURLs(results, s.seeder)
	// Every step below this point was silent. A seeder that admitted no URL, a
	// live toggle that refused, and a provider that returned nothing all left
	// the same trace -- none -- so a node could seed no crawl for hours while
	// its provider answered normally, and the journal could not tell an
	// operator which of the three was happening. The counts are derived from
	// the rows and their URLs, never from the query.
	slog.InfoContext(ctx, msgWebSeedConsidered,
		slog.Int("results", len(results)),
		slog.Int("admitted", len(urls)))
	rejected := 0
	for _, url := range urls {
		if !s.spawnSeedWork(url, ctx, func(seedContext context.Context) {
			s.seeder.Seed(seedContext, []string{url})
		}) {
			rejected++
		}
	}
	if rejected > 0 {
		slog.WarnContext(ctx, msgWebSeedRejected, slog.Int("urls", rejected))
	}
}
