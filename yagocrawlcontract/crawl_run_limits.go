package yagocrawlcontract

func ValidCrawlRunLimits(maxPagesPerHost, maxPagesPerRun int) bool {
	return (maxPagesPerHost == UnlimitedPagesPerHost || maxPagesPerHost > 0) &&
		maxPagesPerRun >= 0
}
