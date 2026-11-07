package crawlorder

type BackoffRunDeferralObserver interface {
	ObserveBackoffRunDeferral()
}

func (c *CrawlOrderConsumer) WithBackoffRunDeferralObserver(
	observer BackoffRunDeferralObserver,
) *CrawlOrderConsumer {
	c.backoffRunDeferralObserver = observer

	return c
}
