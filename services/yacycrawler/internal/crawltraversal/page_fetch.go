package crawltraversal

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

func (c *crawl) fetchPage(
	ctx context.Context,
	rawURL string,
) (crawlcapability.FetchOutcome, error) {
	start := c.clock.Now()
	outcome, err := c.fetch.Fetch(ctx, rawURL)
	if err != nil {
		return crawlcapability.FetchOutcome{}, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	c.observer.FetchObserved(c.clock.Now().Sub(start))
	return outcome, nil
}
