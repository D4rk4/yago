package pipeline

// Observer receives crawl pipeline activity so a caller can record it, for
// example as metrics. Implementations must be safe for concurrent use.
type Observer interface {
	JobStarted()
	JobFinished()
	FetchAttempted()
	FetchSucceeded(bytes int)
	FetchFailed()
	IngestPublished()
}

type noopObserver struct{}

func (noopObserver) JobStarted()        {}
func (noopObserver) JobFinished()       {}
func (noopObserver) FetchAttempted()    {}
func (noopObserver) FetchSucceeded(int) {}
func (noopObserver) FetchFailed()       {}
func (noopObserver) IngestPublished()   {}

type Option func(*Pipeline)

func WithObserver(observer Observer) Option {
	return func(p *Pipeline) {
		if observer != nil {
			p.observer = observer
		}
	}
}
