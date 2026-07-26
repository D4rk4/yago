package pipeline

import "time"

// Observer receives crawl pipeline activity so a caller can record it, for
// example as metrics. Implementations must be safe for concurrent use.
type Observer interface {
	JobStarted()
	JobFinished()
	// FetchAdmissionWaitStarted/Finished bracket the wait on the fleet
	// fetch-start permit and the process page-rate budget, and
	// ObserveFetchAdmissionWait records its duration. These are what separate
	// "the crawl is busy" from "the crawl is rate-limited": a job parked on the
	// governor is still an active job.
	FetchAdmissionWaitStarted()
	FetchAdmissionWaitFinished()
	ObserveFetchAdmissionWait(elapsed time.Duration)
	FetchAttempted()
	FetchSucceeded(bytes int)
	FetchFailed()
	IngestPublished()
}

type noopObserver struct{}

func (noopObserver) JobStarted()                             {}
func (noopObserver) JobFinished()                            {}
func (noopObserver) FetchAdmissionWaitStarted()              {}
func (noopObserver) FetchAdmissionWaitFinished()             {}
func (noopObserver) ObserveFetchAdmissionWait(time.Duration) {}
func (noopObserver) FetchAttempted()                         {}
func (noopObserver) FetchSucceeded(int)                      {}
func (noopObserver) FetchFailed()                            {}
func (noopObserver) IngestPublished()                        {}

type Option func(*Pipeline)

func WithObserver(observer Observer) Option {
	return func(p *Pipeline) {
		if observer != nil {
			p.observer = observer
		}
	}
}
