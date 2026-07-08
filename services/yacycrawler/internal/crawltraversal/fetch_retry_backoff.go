package crawltraversal

import "time"

func (c *crawl) retryDelay(attempt int) time.Duration {
	delay := c.config.FetchRetryFloor << (attempt - 1)
	if delay <= 0 || delay > c.config.FetchRetryCeiling {
		return c.config.FetchRetryCeiling
	}
	return delay
}
