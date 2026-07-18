package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

func validCrawlerLeaseIdentity(workerID string, workerSessionID string) bool {
	return yagocrawlcontract.ValidCrawlerWorkerIdentity(workerID) &&
		yagocrawlcontract.ValidCrawlerSessionIdentity(workerSessionID)
}
