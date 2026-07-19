package main

import "sync/atomic"

type redirectLimit struct {
	maximum atomic.Int64
}

func newRedirectLimit(maximum int) *redirectLimit {
	limit := &redirectLimit{}
	limit.Set(maximum)

	return limit
}

func (limit *redirectLimit) Set(maximum int) {
	if maximum >= 0 {
		limit.maximum.Store(int64(maximum))
	}
}

func (limit *redirectLimit) Current() int {
	return int(limit.maximum.Load())
}
