package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type fakeOwnership struct {
	owns map[string]bool
	err  error
}

func (o fakeOwnership) OwnsProfile(_ context.Context, handle string) (bool, error) {
	if o.err != nil {
		return false, o.err
	}

	return o.owns[handle], nil
}

func runWithOwnership(
	t *testing.T,
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
			SourceURL:     "https://example.org/page",
			ProfileHandle: handle,
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://example.org/page",
				ExtractedText: "body",
			},
		},
		Ack: func(context.Context) error { acked = true; wg.Done(); return nil },
		Nak: func(context.Context) error { naked = true; wg.Done(); return nil },
	}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		documents,
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.CheckOwnership(oracle)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()

	return documents, acked, naked
}

func TestOwnedBatchIsAbsorbed(t *testing.T) {
	documents, acked, naked := runWithOwnership(
		t,
		fakeOwnership{owns: map[string]bool{"h1": true}},
		"h1",
	)
	if documents.calls != 1 || !acked || naked {
		t.Fatalf("owned batch: stored=%d acked=%v naked=%v, want 1/true/false",
			documents.calls, acked, naked)
	}
}

func TestUnownedBatchIsDropped(t *testing.T) {
	documents, acked, naked := runWithOwnership(
		t,
		fakeOwnership{owns: map[string]bool{}},
		"unknown",
	)
	if documents.calls != 0 || !acked || naked {
		t.Fatalf("unowned batch: stored=%d acked=%v naked=%v, want 0/true/false (dropped)",
			documents.calls, acked, naked)
	}
}

func TestOwnershipErrorDefersBatch(t *testing.T) {
	documents, acked, naked := runWithOwnership(
		t,
		fakeOwnership{err: errors.New("vault down")},
		"h1",
	)
	if documents.calls != 0 || acked || !naked {
		t.Fatalf("ownership error: stored=%d acked=%v naked=%v, want 0/false/true (deferred)",
			documents.calls, acked, naked)
	}
}
