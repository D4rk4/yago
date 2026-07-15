package crawlbroker

import (
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

// IngestReceiver hands ingest batches submitted over gRPC to the node's ingest
// consumer. SubmitIngest blocks on Receive until the consumer takes the
// delivery, which is the backpressure the crawler observes.
type IngestReceiver struct {
	out         chan crawlresults.IngestDelivery
	outstanding atomic.Int64
}

func newIngestReceiver() *IngestReceiver {
	return &IngestReceiver{out: make(chan crawlresults.IngestDelivery)}
}

func (r *IngestReceiver) Receive() <-chan crawlresults.IngestDelivery {
	return r.out
}

func (r *IngestReceiver) Outstanding() int {
	return int(r.outstanding.Load())
}

func (r *IngestReceiver) beginIngest() func() {
	r.outstanding.Add(1)
	var once sync.Once

	return func() {
		once.Do(func() {
			r.outstanding.Add(-1)
		})
	}
}
