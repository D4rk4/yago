package pageindex

import (
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func BuildPageStats(page pageparse.ParsedPage) pageparse.PageStats {
	return pageparse.BuildBoundedPageStats(
		page,
		yagocrawlcontract.MaximumDocumentWords,
		255,
		yagocrawlcontract.MaximumDocumentOutlinks,
	)
}
