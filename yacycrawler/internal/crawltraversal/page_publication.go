package crawltraversal

import (
	"context"
	"errors"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

func (c *crawl) publish(ctx context.Context, page crawlcapability.ExtractedPage) error {
	for _, output := range c.outputs {
		policy := c.newBackoff(ctx)
		for {
			err := output.Publish(ctx, page)
			if err == nil {
				break
			}
			var retryable crawlcapability.TransientPublicationError
			if !errors.As(err, &retryable) {
				return fmt.Errorf("publish to %s: %w", output.Name(), err)
			}
			c.observer.PublicationWaited()
			if sleepErr := c.clock.Sleep(ctx, policy.NextBackOff()); sleepErr != nil {
				return fmt.Errorf("await publication retry: %w", sleepErr)
			}
		}
		c.observer.PagePublished(output.Name())
	}
	return nil
}
