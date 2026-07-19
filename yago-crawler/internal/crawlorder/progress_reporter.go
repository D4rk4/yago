package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type RunReport struct {
	Provenance      []byte
	LeaseID         string
	ProfileHandle   string
	ProfileName     string
	State           yagocrawlcontract.CrawlRunState
	Tally           yagocrawlcontract.CrawlRunTally
	RecentOutcomes  yagocrawlcontract.CrawlURLOutcomeHistory
	PagesPerMinute  uint32
	MaxPagesPerHost int
	MaxPagesPerRun  int
}

type ProgressReporter interface {
	ReportRun(ctx context.Context, report RunReport)
}
