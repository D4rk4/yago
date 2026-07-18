package crawlorder

import "github.com/D4rk4/yago/yagocrawlcontract"

// RunTallySource supplies a run's accumulated outcome counts at completion, so the
// finish report carries the run's fetched/indexed/failed/robots-denied/duplicate
// totals rather than an empty tally. Forget drops a run's counts once reported.
type RunTallySource interface {
	Restore(provenance []byte, tally yagocrawlcontract.CrawlRunTally)
	Snapshot(provenance []byte) yagocrawlcontract.CrawlRunTally
	Forget(provenance []byte)
}
