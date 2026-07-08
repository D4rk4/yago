package crawltraversal

import (
	"context"

	"github.com/cenkalti/backoff/v4"
)

func (c *crawl) newBackoff(ctx context.Context) backoff.BackOff {
	exponential := backoff.NewExponentialBackOff()
	exponential.InitialInterval = c.config.PublishRetryFloor
	exponential.MaxInterval = c.config.PublishRetryCeiling
	exponential.MaxElapsedTime = 0
	return backoff.WithContext(exponential, ctx)
}
