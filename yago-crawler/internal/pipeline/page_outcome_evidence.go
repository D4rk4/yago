package pipeline

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pageOutcomeEvidenceFrontier interface {
	DoneWithPageOutcome(crawljob.CrawlJob, yagocrawlcontract.CrawlRunTally, uint32, string)
}
