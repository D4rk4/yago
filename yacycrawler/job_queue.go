package yacycrawler

import (
	"context"
	"fmt"
)

type JobSink interface {
	Enqueue(ctx context.Context, job CrawlJob) error
}

type JobSource interface {
	Jobs() <-chan CrawlJob
}

type JobQueue struct {
	ch chan CrawlJob
}

func NewJobQueue(capacity int) *JobQueue {
	return &JobQueue{ch: make(chan CrawlJob, capacity)}
}

func (q *JobQueue) Enqueue(ctx context.Context, job CrawlJob) error {
	select {
	case q.ch <- job:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("enqueue job: %w", ctx.Err())
	}
}

func (q *JobQueue) Jobs() <-chan CrawlJob {
	return q.ch
}

func (q *JobQueue) Close() {
	close(q.ch)
}
