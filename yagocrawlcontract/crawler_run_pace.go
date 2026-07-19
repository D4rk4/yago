package yagocrawlcontract

func ParseCrawlerRunPagesPerMinute(raw string) (uint32, error) {
	return parseBoundedUint32(
		raw,
		"run pages per minute",
		MaximumCrawlerRunPagesPerMinute,
	)
}
