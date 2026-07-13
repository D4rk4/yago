package frontier

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

func (f *Frontier) Take(ctx context.Context) (crawljob.CrawlJob, bool) {
	for {
		job, wait, result := f.claimNext(ctx, time.Now())
		switch result {
		case takeClaimed:
			f.wake()

			return job, true
		case takeStopped:
			return crawljob.CrawlJob{}, false
		case takeClosed:
			f.wake()

			return crawljob.CrawlJob{}, false
		case takeWaiting:
			if !f.waitForChange(ctx, wait) {
				return crawljob.CrawlJob{}, false
			}
		}
	}
}

func (f *Frontier) waitForChange(ctx context.Context, wait time.Duration) bool {
	var wakeup <-chan time.Time
	var timer *time.Timer
	if wait > 0 {
		timer = time.NewTimer(wait)
		wakeup = timer.C
	}
	select {
	case <-ctx.Done():
		if timer != nil {
			timer.Stop()
		}

		return false
	case <-f.signal:
	case <-wakeup:
	}
	if timer != nil {
		timer.Stop()
	}

	return true
}
