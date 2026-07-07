package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

const removalURL = "https://example.org/gone"

type recordingPurger struct {
	mu     sync.Mutex
	purged [][]yagomodel.Hash
	err    error
}

func (p *recordingPurger) Purge(_ context.Context, urls []yagomodel.Hash) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.purged = append(p.purged, urls)

	return p.err
}

func (p *recordingPurger) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.purged)
}

func deliverRemoval(
	t *testing.T,
	purger crawlresults.URLPurger,
	oracle crawlresults.OwnershipCheck,
	handle string,
) (documents *recordingDocumentReceiver, acked, naked bool) {
	t.Helper()
	documents = &recordingDocumentReceiver{}
	var wg sync.WaitGroup
	wg.Add(1)
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL:     removalURL,
			ProfileHandle: handle,
			Removed:       true,
		},
		Ack: func(context.Context) error { acked = true; wg.Done(); return nil },
		Nak: func(context.Context) error { naked = true; wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream, documents, &recordingURLReceiver{}, &recordingPostingReceiver{},
	)
	consumer.PurgeURLs(purger)
	if oracle != nil {
		consumer.CheckOwnership(oracle)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()

	return documents, acked, naked
}

func TestRemovalBatchPurgesSourceURLAndAcks(t *testing.T) {
	purger := &recordingPurger{}
	documents, acked, naked := deliverRemoval(
		t, purger, fakeOwnership{owns: map[string]bool{"h1": true}}, "h1",
	)
	if !acked || naked {
		t.Fatalf("removal batch: acked=%v naked=%v, want acked", acked, naked)
	}
	if documents.calls != 0 {
		t.Fatalf("a tombstone must not store a document, calls = %d", documents.calls)
	}
	if purger.calls() != 1 {
		t.Fatalf("purger calls = %d, want 1", purger.calls())
	}
	want, err := yagomodel.HashURL(removalURL)
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	if len(purger.purged[0]) != 1 || purger.purged[0][0] != want.Hash() {
		t.Fatalf("purged = %v, want the hashed source url", purger.purged)
	}
}

func TestRemovalBatchFromUnownedProfileIsRejected(t *testing.T) {
	purger := &recordingPurger{}
	documents, acked, naked := deliverRemoval(
		t, purger, fakeOwnership{owns: map[string]bool{}}, "unknown",
	)
	if !acked || naked {
		t.Fatalf("unowned removal: acked=%v naked=%v, want dropped (acked)", acked, naked)
	}
	if purger.calls() != 0 {
		t.Fatalf("an unowned tombstone must not purge, calls = %d", purger.calls())
	}
	if documents.calls != 0 {
		t.Fatalf("a tombstone must never store a document, calls = %d", documents.calls)
	}
}

func TestRemovalBatchRedeliversOnPurgeError(t *testing.T) {
	purger := &recordingPurger{err: errors.New("vault down")}
	_, acked, naked := deliverRemoval(
		t, purger, fakeOwnership{owns: map[string]bool{"h1": true}}, "h1",
	)
	if acked || !naked {
		t.Fatalf("purge error: acked=%v naked=%v, want redelivered (naked)", acked, naked)
	}
	if purger.calls() != 1 {
		t.Fatalf("purger calls = %d, want 1 (attempted)", purger.calls())
	}
}

func settleRemoval(
	t *testing.T,
	batch yagocrawlcontract.IngestBatch,
	configure func(*crawlresults.IngestConsumer),
	ack, nak func(context.Context) error,
) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	stream.out <- crawlresults.IngestDelivery{
		Batch: batch,
		Ack:   func(ctx context.Context) error { defer wg.Done(); return ack(ctx) },
		Nak:   func(ctx context.Context) error { defer wg.Done(); return nak(ctx) },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	if configure != nil {
		configure(consumer)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()
}

func TestRemovalBatchWithDefaultPurgerAcks(t *testing.T) {
	acked := make(chan bool, 1)
	settleRemoval(
		t,
		yagocrawlcontract.IngestBatch{SourceURL: removalURL, Removed: true},
		nil,
		func(context.Context) error { acked <- true; return nil },
		func(context.Context) error { acked <- false; return nil },
	)
	if !<-acked {
		t.Fatal("a removal with the default no-op purger must still ack")
	}
}

func TestRemovalBatchWithEmptySourceURLIsRejected(t *testing.T) {
	acked := make(chan bool, 1)
	settleRemoval(
		t,
		yagocrawlcontract.IngestBatch{SourceURL: "", Removed: true},
		nil,
		func(context.Context) error { acked <- true; return nil },
		func(context.Context) error { acked <- false; return nil },
	)
	if !<-acked {
		t.Fatal("a removal missing its source url must be dropped (acked)")
	}
}

func TestRemovalBatchRedeliversOnOwnershipError(t *testing.T) {
	settled := make(chan bool, 1)
	settleRemoval(
		t,
		yagocrawlcontract.IngestBatch{SourceURL: removalURL, ProfileHandle: "h1", Removed: true},
		func(c *crawlresults.IngestConsumer) {
			c.CheckOwnership(fakeOwnership{err: errors.New("vault down")})
		},
		func(context.Context) error { settled <- true; return nil },
		func(context.Context) error { settled <- false; return nil },
	)
	if <-settled {
		t.Fatal("an ownership check error must redeliver (nak), not ack")
	}
}

func TestRemovalBatchToleratesAckFailure(t *testing.T) {
	settleRemoval(
		t,
		yagocrawlcontract.IngestBatch{SourceURL: removalURL, ProfileHandle: "h1", Removed: true},
		func(c *crawlresults.IngestConsumer) {
			c.CheckOwnership(fakeOwnership{owns: map[string]bool{"h1": true}})
			c.PurgeURLs(&recordingPurger{})
		},
		func(context.Context) error { return errors.New("ack failed") },
		func(context.Context) error { t.Error("unexpected nak on a purged removal"); return nil },
	)
}

func TestNonRemovalBatchStillStoresDocumentAndDoesNotPurge(t *testing.T) {
	documents := &recordingDocumentReceiver{}
	purger := &recordingPurger{}
	var wg sync.WaitGroup
	wg.Add(1)
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	stream.out <- crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://example.org",
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://example.org",
				ExtractedText: "body",
			},
		},
		Ack: func(context.Context) error { wg.Done(); return nil },
		Nak: func(context.Context) error { wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream, documents, &recordingURLReceiver{}, &recordingPostingReceiver{},
	)
	consumer.PurgeURLs(purger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()

	if documents.calls != 1 {
		t.Fatalf("a normal batch must still store its document, calls = %d", documents.calls)
	}
	if purger.calls() != 0 {
		t.Fatalf("a normal batch must not purge, calls = %d", purger.calls())
	}
}
