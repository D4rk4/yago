package worker

import "github.com/D4rk4/yago/yagocrawlcontract"

type orderExecutionSummary struct {
	tally yagocrawlcontract.CrawlRunTally
	seeds seedAdmission
}
