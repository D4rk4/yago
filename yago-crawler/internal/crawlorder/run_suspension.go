package crawlorder

func (c *CrawlOrderConsumer) SuspendActiveRuns() {
	for _, provenance := range c.active.provenances() {
		c.frontier.Suspend(provenance)
	}
}
