package websearch

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	webSeedConcurrentWrites = 2
	webSeedPendingPerWorker = 64
	webSeedWriteTimeout     = 10 * time.Second
)

var webSeedProcessAdmission = newWebSeedAdmission(webSeedConcurrentWrites)

type webSeedAdmission struct {
	mutex    sync.Mutex
	pending  chan webSeedWork
	admitted map[string]struct{}
}

type webSeedWork struct {
	key string
	run func(context.Context)
}

func newWebSeedAdmission(capacity int) *webSeedAdmission {
	admission := &webSeedAdmission{
		pending:  make(chan webSeedWork, capacity*webSeedPendingPerWorker),
		admitted: make(map[string]struct{}, capacity*webSeedPendingPerWorker),
	}
	for range capacity {
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
