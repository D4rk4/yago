package worker

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
)

const maximumPageAttemptDuration = 5 * time.Second

type pageProcessingResult struct {
	state             frontier.State
	retryAt           time.Time
	pendingIngestJSON []byte
	stage             string
}

func (worker *Worker) deferredPageResult(page frontier.Page, stage string) pageProcessingResult {
	return pageProcessingResult{
		state:   worker.retryOrFail(page, context.DeadlineExceeded, 0),
		retryAt: retryDeadline(page, "", time.Now()),
		stage:   stage,
	}
}
