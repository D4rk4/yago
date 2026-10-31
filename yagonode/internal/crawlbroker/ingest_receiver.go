package crawlbroker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

// IngestReceiver hands ingest batches submitted over gRPC to the node's ingest
// consumer. SubmitIngest blocks on Receive until the consumer takes the
// delivery, which is the backpressure the crawler observes.
type IngestReceiver struct {
	out             chan crawlresults.IngestDelivery
	outstanding     atomic.Int64
	importSlotsOnce sync.Once
	importSlots     chan struct{}
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

func (r *IngestReceiver) MaximumConcurrentImports() int {
	return yagocrawlcontract.MaximumIngestBatchRecords
}

func (r *IngestReceiver) concurrentImportSlots() chan struct{} {
	r.importSlotsOnce.Do(func() {
		r.importSlots = make(chan struct{}, r.MaximumConcurrentImports())
	})
	return r.importSlots
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

func (r *IngestReceiver) SubmitImport(
	ctx context.Context,
	batch yagocrawlcontract.IngestBatch,
	profile yagocrawlcontract.CrawlProfile,
) error {
	if batch.ProfileHandle == "" || batch.ProfileHandle != profile.Handle {
		return errors.New("document import profile does not match its handle")
	}
	slots := r.concurrentImportSlots()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("admit imported ingest batch: %w", ctx.Err())
	}
	defer func() { <-slots }()
	encoded, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("size imported ingest batch: %w", err)
	}
	finish := r.beginIngest()
	result := make(chan ingestOutcome, 1)
	delivery := crawlresults.IngestDelivery{
		Batch:         batch,
		CrawlProfile:  &profile,
		ImportOnly:    true,
		BatchJSONSize: len(encoded),
		Ack: func(context.Context) error {
			finish()
			result <- ingestOutcome{}
			return nil
		},
		Reject: func(_ context.Context, reason string) error {
			finish()
			result <- ingestOutcome{err: fmt.Errorf("import rejected: %s", reason)}
			return nil
		},
		Nak: func(context.Context) error {
			finish()
			result <- ingestOutcome{err: errIngestDeferred}
			return nil
		},
	}
	select {
	case r.out <- delivery:
	case <-ctx.Done():
		finish()
		return fmt.Errorf("submit imported ingest delivery: %w", ctx.Err())
	}
	select {
	case outcome := <-result:
		return outcome.err
	case <-ctx.Done():
		return fmt.Errorf("wait for imported ingest result: %w", ctx.Err())
	}
}
