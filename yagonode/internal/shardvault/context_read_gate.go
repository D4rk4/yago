package shardvault

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const globalReadRetryInterval = 2 * time.Millisecond

func acquireGlobalRead(ctx context.Context, gate *sync.RWMutex) error {
	acquired, err := tryAcquireGlobalRead(ctx, gate)
	if err != nil {
		return err
	}
	if acquired {
		return nil
	}
	ticker := time.NewTicker(globalReadRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("acquire global read: %w", ctx.Err())
		case <-ticker.C:
			acquired, err := tryAcquireGlobalRead(ctx, gate)
			if err != nil {
				return err
			}
			if acquired {
				return nil
			}
		}
	}
}

func tryAcquireGlobalRead(ctx context.Context, gate *sync.RWMutex) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("acquire global read: %w", err)
	}
	if !gate.TryRLock() {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		gate.RUnlock()

		return false, fmt.Errorf("acquire global read: %w", err)
	}

	return true, nil
}
