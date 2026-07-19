package frontier

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (f *Frontier) DoneWithReason(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
	reason string,
) {
	f.completePage(work, outcome, 0, reason)
}
