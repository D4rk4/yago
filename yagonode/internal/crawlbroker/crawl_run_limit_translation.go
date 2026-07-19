package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func crawlRunLimitsFromProto(
	maxPagesPerHost *int64,
	maxPagesPerRun *uint64,
) (int, int, bool, error) {
	if maxPagesPerHost == nil && maxPagesPerRun == nil {
		return 0, 0, false, nil
	}
	if maxPagesPerHost == nil || maxPagesPerRun == nil {
		return 0, 0, false, fmt.Errorf("incomplete crawl run limits")
	}
	maximumInt := uint64(^uint(0) >> 1)
	if *maxPagesPerHost > int64(maximumInt) || *maxPagesPerRun > maximumInt {
		return 0, 0, false, fmt.Errorf("crawl run limit exceeds platform range")
	}
	perHost := int(*maxPagesPerHost)
	perRun := int(*maxPagesPerRun)
	if !yagocrawlcontract.ValidCrawlRunLimits(perHost, perRun) {
		return 0, 0, false, fmt.Errorf("invalid crawl run limits")
	}

	return perHost, perRun, true, nil
}
