package yagocrawlcontract

func (o CrawlOrder) EffectiveMaxPagesPerRun(fallback int) int {
	maximum := o.Profile.EffectiveMaxPagesPerRun(fallback)
	if o.Priority != CrawlOrderPriorityAutomaticDiscovery || o.Profile.MaxPagesPerHost <= 0 {
		return maximum
	}
	if maximum == 0 || maximum > o.Profile.MaxPagesPerHost {
		return o.Profile.MaxPagesPerHost
	}

	return maximum
}
