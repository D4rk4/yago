package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// RunReport is a worker's snapshot of one crawl run's lifecycle, keyed by the
// order provenance token so the node and worker agree on run identity without the
// node owning the worker's in-memory frontier.
type RunReport struct {
	Provenance    []byte
	ProfileHandle string
	ProfileName   string
	State         yagocrawlcontract.CrawlRunState
	Tally         yagocrawlcontract.CrawlRunTally
}

// ProgressReporter receives the run lifecycle snapshots the consumer emits as a
// run starts and finishes. Reporting is best-effort observability and must never
// block or fail the crawl.
type ProgressReporter interface {
	ReportRun(ctx context.Context, report RunReport)
}

type noopProgressReporter struct{}

func (noopProgressReporter) ReportRun(context.Context, RunReport) {}
