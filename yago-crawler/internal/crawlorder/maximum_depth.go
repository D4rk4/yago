package crawlorder

func (c *CrawlOrderConsumer) WithMaximumDepth(maximum int) *CrawlOrderConsumer {
	if maximum > 0 {
		c.maximumDepth = maximum
	}

	return c
}
