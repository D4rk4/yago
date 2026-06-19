package yacycrawler

import (
	"context"
)

type JobSink interface {
	Enqueue(ctx context.Context, job CrawlJob) error
}

type JobSource interface {
	Jobs() <-chan CrawlJob
}

type JobQueue struct {
	queue *BoundedQueue[CrawlJob]
}

func NewJobQueue(capacity int) *JobQueue {
	return &JobQueue{queue: NewBoundedQueue[CrawlJob](capacity)}
}

func (q *JobQueue) Enqueue(ctx context.Context, job CrawlJob) error {
	return q.queue.Publish(ctx, job)
}

func (q *JobQueue) Jobs() <-chan CrawlJob {
	return q.queue.Receive()
}

func (q *JobQueue) Close() {
	q.queue.Close()
}
