package worker

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

const (
	maximumPageAttemptDuration = 5 * time.Second
	maximumPageWorkersPerHost  = 1
)

type pageProcessingResult struct {
	state             frontier.State
	retryAt           time.Time
	rateLimited       bool
	pendingIngestJSON []byte
	outcome           frontier.PageOutcome
	stage             string
}

func (worker *Worker) deferredPageResult(page frontier.Page, stage string) pageProcessingResult {
	return pageProcessingResult{
		state:   worker.retryOrFail(page, context.DeadlineExceeded, 0),
		retryAt: retryDeadline(page, "", time.Now()),
		stage:   stage,
	}
}
