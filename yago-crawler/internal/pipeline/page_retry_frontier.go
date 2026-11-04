package pipeline

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

type pageRetryFrontier interface {
	Defer(context.Context, crawljob.CrawlJob, time.Time) (bool, bool)
}

type pageRetryObserver interface {
	PageRetryScheduled()
}
