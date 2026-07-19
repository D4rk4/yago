package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type artifactURLReceiver struct {
	rows []yagomodel.URIMetadataRow
}

func (r *artifactURLReceiver) Receive(
	_ context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	r.rows = append(r.rows, rows...)

	return urlmeta.Receipt{}, nil
}

type artifactPostingReceiver struct {
	postings []yagomodel.RWIPosting
}

func (r *artifactPostingReceiver) Receive(
	_ context.Context,
	postings []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	r.postings = append(r.postings, postings...)

	return rwi.Receipt{}, nil
}

func TestGroupedRemovalUsesTombstonePath(t *testing.T) {
	purger := &recordingPurger{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 2)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.PurgeURLs(purger)

	var settled sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(yagocrawlcontract.IngestBatch{
		SourceURL: removalURL,
		Removed:   true,
	}, &settled, counter, nil)
	stream.out <- groupDelivery(metaOnlyBatch("https://example.org/live"), &settled, counter, nil)
	drainGroup(consumer, &settled)

	if purger.calls() != 1 {
		t.Fatalf("purger calls = %d, want 1", purger.calls())
	}
	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want 2/0", acks, naks)
	}
}

func TestGroupedLiveRemovalCollisionUsesNewestObservation(t *testing.T) {
	const sourceURL = "https://example.org/collision"
	live := groupDocBatch(sourceURL, clusteredPageText)
	live.Document.FetchedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	live.ObservationID = "live"
	live.ObservedAt = live.Document.FetchedAt
	removed := yagocrawlcontract.IngestBatch{
		SourceURL:     sourceURL,
		ObservationID: "removed",
		ObservedAt:    live.ObservedAt.Add(time.Hour),
		Removed:       true,
	}
	newerLive := live
	newerLive.ObservationID = "newer-live"
	newerLive.ObservedAt = removed.ObservedAt.Add(time.Hour)
	tests := []struct {
		name       string
		batches    []yagocrawlcontract.IngestBatch
		wantPurges int
		wantDocs   int
	}{
		{"newer removal last", []yagocrawlcontract.IngestBatch{live, removed}, 1, 0},
		{"newer removal first", []yagocrawlcontract.IngestBatch{removed, live}, 1, 0},
		{"newer live last", []yagocrawlcontract.IngestBatch{removed, newerLive}, 0, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			purger := &recordingPurger{}
			documents := &recordingDocumentReceiver{}
			stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 2)}
			consumer := crawlresults.NewIngestConsumer(
				stream,
				documents,
				&recordingURLReceiver{},
				&recordingPostingReceiver{},
			)
			consumer.PurgeURLs(purger)

			var settled sync.WaitGroup
			counter := &settleCounter{}
			for _, batch := range test.batches {
				stream.out <- groupDelivery(batch, &settled, counter, nil)
			}
			drainGroup(consumer, &settled)

			if purger.calls() != test.wantPurges || documents.calls != test.wantDocs {
				t.Fatalf(
					"purges=%d documents=%d, want %d/%d",
					purger.calls(),
					documents.calls,
					test.wantPurges,
					test.wantDocs,
				)
			}
			if acks, naks := counter.counts(); acks != 2 || naks != 0 {
				t.Fatalf("acks=%d naks=%d, want 2/0", acks, naks)
			}
		})
	}
}

func TestGroupedDuplicateURLCommitsNewestObservationArtifacts(t *testing.T) {
	const sourceURL = "https://example.org/page"
	firstObservedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	documents := &recordingDocumentReceiver{}
	urls := &artifactURLReceiver{}
	postings := &artifactPostingReceiver{}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 2)}
	consumer := crawlresults.NewIngestConsumer(stream, documents, urls, postings)

	batch := func(version string, observedAt time.Time) yagocrawlcontract.IngestBatch {
		return yagocrawlcontract.IngestBatch{
			SourceURL:     sourceURL,
			ProfileHandle: version,
			ObservationID: version,
			ObservedAt:    observedAt,
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: sourceURL,
				ExtractedText: version,
			},
			Metadata: []yagomodel.URIMetadataRow{{
				Properties: map[string]string{"version": version},
			}},
			Postings: []yagomodel.RWIPosting{{WordHash: yagomodel.WordHash(version)}},
		}
	}

	var settled sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(
		batch("second", firstObservedAt.Add(time.Hour)),
		&settled,
		counter,
		nil,
	)
	stream.out <- groupDelivery(
		batch("first", firstObservedAt),
		&settled,
		counter,
		errors.New("ack failed"),
	)
	drainGroup(consumer, &settled)

	if acks, naks := counter.counts(); acks != 2 || naks != 0 {
		t.Fatalf("acks=%d naks=%d, want 2/0", acks, naks)
	}
	if documents.calls != 1 || documents.doc.ExtractedText != "second" {
		t.Fatalf("document calls=%d text=%q, want one second version", documents.calls,
			documents.doc.ExtractedText)
	}
	if len(urls.rows) != 1 || urls.rows[0].Properties["version"] != "second" {
		t.Fatalf("metadata = %#v, want second version only", urls.rows)
	}
	if len(postings.postings) != 1 ||
		postings.postings[0].WordHash != yagomodel.WordHash("second") {
		t.Fatalf("postings = %#v, want second version only", postings.postings)
	}
}

func TestGroupedDuplicateURLRedeliversEveryDelivery(t *testing.T) {
	const sourceURL = "https://example.org/page"
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 2)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{err: errors.New("store failed")},
		&recordingPostingReceiver{},
	)

	var settled sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(metaOnlyBatch(sourceURL), &settled, counter, nil)
	stream.out <- groupDelivery(metaOnlyBatch(sourceURL), &settled, counter, nil)
	drainGroup(consumer, &settled)

	if acks, naks := counter.counts(); acks != 0 || naks != 2 {
		t.Fatalf("acks=%d naks=%d, want 0/2", acks, naks)
	}
}
