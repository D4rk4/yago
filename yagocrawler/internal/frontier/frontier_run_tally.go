package frontier

// RunTally records per-run frontier outcomes — currently a duplicate URL skipped
// because the run has already visited it. The provenance is the run's shared
// identity token. Implementations must be safe for concurrent use.
type RunTally interface {
	Duplicate(provenance []byte)
}

type noopRunTally struct{}

func (noopRunTally) Duplicate([]byte) {}

// WithRunTally installs a per-run outcome tally that receives duplicate-skip
// events. A nil tally is ignored so the frontier keeps its silent default.
func WithRunTally(tally RunTally) Option {
	return func(f *Frontier) {
		if tally != nil {
			f.state.tally = tally
		}
	}
}
