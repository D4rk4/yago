package websearch

import (
	"context"
	"time"
)

const (
	webSeedConcurrentWrites = 2
	webSeedWriteTimeout     = 10 * time.Second
)

var webSeedProcessAdmission = newWebSeedAdmission(webSeedConcurrentWrites)

type webSeedAdmission struct {
	slots  chan struct{}
	launch func(func())
}

func newWebSeedAdmission(capacity int) *webSeedAdmission {
	return &webSeedAdmission{
		slots:  make(chan struct{}, capacity),
		launch: func(work func()) { go work() },
	}
}

func (a *webSeedAdmission) try(work func()) bool {
	select {
	case a.slots <- struct{}{}:
		a.launch(func() {
			defer func() { <-a.slots }()
			work()
		})

		return true
	default:
		return false
	}
}

func (s *FallbackSearcher) seedWebResults(ctx context.Context, results []Result) {
	urls := resultURLs(results)
	seedContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), webSeedWriteTimeout)
	if !s.spawnSeedWork(func() {
		defer cancel()
		s.seeder.Seed(seedContext, urls)
	}) {
		cancel()
	}
}
