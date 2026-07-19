package frontier

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (f *Frontier) DoneWithPageOutcome(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	httpStatus uint32,
	reason string,
) {
	f.completePage(work, outcome, httpStatus, reason)
}
