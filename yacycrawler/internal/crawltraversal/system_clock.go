package crawltraversal

import (
	"context"
	"time"
)

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}

func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return contextError(ctx)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return contextError(ctx)
	case <-timer.C:
		return nil
	}
}
