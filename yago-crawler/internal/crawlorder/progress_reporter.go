package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type RunReport struct {
	Provenance     []byte
	LeaseID        string
	ProfileHandle  string
	ProfileName    string
	State          yagocrawlcontract.CrawlRunState
	Tally          yagocrawlcontract.CrawlRunTally
	PagesPerMinute uint32
}

type ProgressReporter interface {
	ReportRun(ctx context.Context, report RunReport)
}
