package main

import "sync/atomic"

type workerConcurrency struct {
	desired atomic.Int64
	change  chan struct{}
}

func newWorkerConcurrency(initial int) *workerConcurrency {
	control := &workerConcurrency{change: make(chan struct{}, 1)}
	control.desired.Store(int64(initial))

	return control
}

func (c *workerConcurrency) Set(workers int) {
	if c == nil || workers < 1 {
		return
	}
	previous := c.desired.Swap(int64(workers))
	if previous == int64(workers) {
		return
	}
	select {
	case c.change <- struct{}{}:
	default:
	}
}

func (c *workerConcurrency) Current() int {
	if c == nil {
		return 0
	}

	return int(c.desired.Load())
}

func (c *workerConcurrency) Changes() <-chan struct{} {
	if c == nil {
		return nil
	}

	return c.change
}

func (c *workerConcurrency) DrainChanges() {
	if c == nil {
		return
	}
	for {
		select {
		case <-c.change:
		default:
			return
		}
	}
}
