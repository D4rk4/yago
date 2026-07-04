package pipeline

// RunTally records per-run crawl outcomes so a run's totals can be reported when
// it finishes. The provenance is the run's shared identity token, carried on every
// job. Implementations must be safe for concurrent use.
type RunTally interface {
	Fetched(provenance []byte)
	Indexed(provenance []byte)
	Failed(provenance []byte)
	RobotsDenied(provenance []byte)
}

type noopRunTally struct{}

func (noopRunTally) Fetched([]byte) {}

func (noopRunTally) Indexed([]byte) {}

func (noopRunTally) Failed([]byte) {}

func (noopRunTally) RobotsDenied([]byte) {}

// WithRunTally installs a per-run outcome tally. A nil tally is ignored so the
// pipeline keeps its silent default.
func WithRunTally(tally RunTally) Option {
	return func(p *Pipeline) {
		if tally != nil {
			p.tally = tally
		}
	}
}
