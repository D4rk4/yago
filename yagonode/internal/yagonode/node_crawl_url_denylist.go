package yagonode

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

func (r *crawlRuntime) useCrawlURLDenylist(store *urldenylist.Store) {
	r.broker.SetURLDenylistSource(crawlURLDenylistSource(store))
}

func crawlURLDenylistSource(
	store *urldenylist.Store,
) crawlbroker.CrawlURLDenylistSource {
	return func() (yagocrawlcontract.CrawlURLDenylist, error) {
		exactURLs, domains := store.Snapshot().Values()

		return yagocrawlcontract.NewCrawlURLDenylist(exactURLs, domains)
	}
}

func attachCrawlURLDenylist(runtime crawlProcess, store *urldenylist.Store) {
	target, ok := runtime.(interface {
		useCrawlURLDenylist(*urldenylist.Store)
	})
	if ok {
		target.useCrawlURLDenylist(store)
	}
}
