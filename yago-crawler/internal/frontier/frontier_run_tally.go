package frontier

import "github.com/D4rk4/yago/yagocrawlcontract"

// RunTally records per-run frontier outcomes — currently a duplicate URL skipped
// because the run has already visited it. The provenance is the run's shared
// identity token. Implementations must be safe for concurrent use.
type RunTally interface {
	Commit(provenance []byte, tally yagocrawlcontract.CrawlRunTally)
	Snapshot(provenance []byte) yagocrawlcontract.CrawlRunTally
	Restore(provenance []byte, tally yagocrawlcontract.CrawlRunTally)
}

type noopRunTally struct{}

func (noopRunTally) Commit([]byte, yagocrawlcontract.CrawlRunTally) {}

func (noopRunTally) Snapshot([]byte) yagocrawlcontract.CrawlRunTally {
	return yagocrawlcontract.CrawlRunTally{}
}

func (noopRunTally) Restore([]byte, yagocrawlcontract.CrawlRunTally) {}

// WithRunTally installs a per-run outcome tally that receives duplicate-skip
// events. A nil tally is ignored so the frontier keeps its silent default.
func WithRunTally(tally RunTally) Option {
	return func(f *Frontier) {
		if tally != nil {
			f.state.tally = tally
		}
	}
}
