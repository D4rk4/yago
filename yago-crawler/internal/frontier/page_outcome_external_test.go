package frontier_test

import "github.com/D4rk4/yago/yagocrawlcontract"

func successfulPageOutcome() yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{}
}

func failedPageOutcome() yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{Failed: 1}
}
