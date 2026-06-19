package yacycrawler

import (
	"context"
	"errors"
	"fmt"
)

var ErrQueueClosed = errors.New("queue closed")

type Publisher[T any] interface {
	Publish(ctx context.Context, msg T) error
}

type Receiver[T any] interface {
	Receive() <-chan T
}

type BoundedQueue[T any] struct {
	ch chan T
}

func NewBoundedQueue[T any](capacity int) *BoundedQueue[T] {
	return &BoundedQueue[T]{ch: make(chan T, capacity)}
}

func (q *BoundedQueue[T]) Publish(ctx context.Context, msg T) error {
	select {
	case q.ch <- msg:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("publish: %w", ctx.Err())
	}
}

func (q *BoundedQueue[T]) Receive() <-chan T {
	return q.ch
}

func (q *BoundedQueue[T]) Close() {
	close(q.ch)
}
