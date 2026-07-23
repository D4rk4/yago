package crawlresults

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type profileFetchRecorder interface {
	RecordProfileFetch(
		context.Context,
		string,
		yagocrawlcontract.CrawlProfile,
		time.Time,
		time.Time,
	) error
}

type profileFetchBatchRecorder interface {
	RecordProfileFetches(
		context.Context,
		[]string,
		[]yagocrawlcontract.CrawlProfile,
		[]time.Time,
		[]time.Time,
	) error
}
