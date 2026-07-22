package peernews

import (
	"context"
	"fmt"
)

type newsWritePermit chan struct{}

func newNewsWritePermit() newsWritePermit {
	permit := make(newsWritePermit, 1)
	permit <- struct{}{}

	return permit
}

func (p newsWritePermit) Acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("acquire news write permit: %w", ctx.Err())
	case <-p:
		return nil
	}
}

func (p newsWritePermit) Release() {
	p <- struct{}{}
}
