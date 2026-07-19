package crawlresults_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type recordingFetchRecorder struct {
	calls    int
	url      string
	handle   string
	fetched  time.Time
	modified time.Time
}

func (r *recordingFetchRecorder) RecordFetchWithSourceModified(
	_ context.Context,
	url, profileHandle string,
	fetchedAt, sourceModifiedAt time.Time,
) error {
	r.calls++
	r.url = url
	r.handle = profileHandle
	r.fetched = fetchedAt
	r.modified = sourceModifiedAt

	return nil
}

func (r *recordingFetchRecorder) RecordFetch(
	_ context.Context,
	url, profileHandle string,
	fetchedAt time.Time,
) error {
	r.calls++
	r.url = url
	r.handle = profileHandle
	r.fetched = fetchedAt

	return nil
}

func runWithRecorder(
	t *testing.T,
	batch yagocrawlcontract.IngestBatch,
) *recordingFetchRecorder {
	t.Helper()
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	wg.Add(1)
	stream.out <- crawlresults.IngestDelivery{
		Batch: batch,
		Ack:   func(context.Context) error { wg.Done(); return nil },
		Nak:   func(context.Context) error { wg.Done(); return nil },
	}
	recorder := &recordingFetchRecorder{}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.RecordFetches(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	wg.Wait()

	return recorder
}

func TestRecordFetchCapturesAbsorbedPage(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	modifiedAt := fetchedAt.Add(-time.Hour)
	recorder := runWithRecorder(t, yagocrawlcontract.IngestBatch{
		SourceURL:        "https://example.org/page",
		ProfileHandle:    "handle-1",
		SourceModifiedAt: modifiedAt,
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org/page",
			ExtractedText: "body",
			FetchedAt:     fetchedAt,
		},
	})
	if recorder.calls != 1 {
		t.Fatalf("recorder calls = %d, want 1", recorder.calls)
	}
	if recorder.url != "https://example.org/page" ||
		recorder.handle != "handle-1" ||
		!recorder.fetched.Equal(fetchedAt) || !recorder.modified.Equal(modifiedAt) {
		t.Fatalf("recorded (%q, %q, %v, %v), want page/handle-1/%v/%v",
			recorder.url, recorder.handle, recorder.fetched, recorder.modified,
			fetchedAt, modifiedAt)
	}
}

func TestRecordFetchSkipsMetadataOnlyBatch(t *testing.T) {
	recorder := runWithRecorder(t, yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/page",
	})
	if recorder.calls != 0 {
		t.Fatalf("recorder calls = %d for metadata-only batch, want 0", recorder.calls)
	}
}

func TestRecordFetchSkipsBatchWithoutFetchTime(t *testing.T) {
	recorder := runWithRecorder(t, yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/page",
		ProfileHandle: "handle-1",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org/page",
			ExtractedText: "body",
		},
	})
	if recorder.calls != 0 {
		t.Fatalf("recorder calls = %d without fetch time, want 0", recorder.calls)
	}
}
