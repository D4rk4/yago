package pageindex

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

func BuildPageStats(page pageparse.ParsedPage) pageparse.PageStats {
	return pageparse.BuildBoundedPageStats(
		page,
		yagocrawlcontract.MaximumDocumentWords,
		255,
		yagocrawlcontract.MaximumDocumentOutlinks,
	)
}
