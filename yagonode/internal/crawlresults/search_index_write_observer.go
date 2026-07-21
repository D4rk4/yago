package crawlresults

import "time"

type SearchIndexWriteObserver interface {
	ObserveSearchIndexWrite(time.Duration, int, bool)
}

func (c *IngestConsumer) ObserveSearchIndexWrites(observer SearchIndexWriteObserver) {
	if observer != nil {
		c.indexWrites = observer
	}
}
