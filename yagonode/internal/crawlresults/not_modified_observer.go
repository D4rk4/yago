package crawlresults

type NotModifiedObserver interface {
	ObserveNotModifiedRecorded()
	ObserveNotModifiedRefused()
}

func (c *IngestConsumer) observeNotModified(recorded bool) {
	observer, ok := c.observer.(NotModifiedObserver)
	if !ok {
		return
	}
	if recorded {
		observer.ObserveNotModifiedRecorded()

		return
	}
	observer.ObserveNotModifiedRefused()
}
