package crawlbroker

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestSubmitIngestRefusesInvalidPayloadsBeforeDelivery(t *testing.T) {
	cases := map[string]yagocrawlcontract.IngestBatch{
		"removal beside not-modified": func() yagocrawlcontract.IngestBatch {
			batch := notModifiedIngestBatch()
			batch.Removed = true
			return batch
		}(),
		"document beside not-modified": func() yagocrawlcontract.IngestBatch {
			batch := notModifiedIngestBatch()
			batch.Document.Title = "Changed page"
			return batch
		}(),
		"not-modified without observation identity": func() yagocrawlcontract.IngestBatch {
			batch := notModifiedIngestBatch()
			batch.ObservationID = ""
			return batch
		}(),
		"not-modified with an incomplete revision": func() yagocrawlcontract.IngestBatch {
			batch := notModifiedIngestBatch()
			batch.NotModified.Revision.ExtractionGeneration = 0
			return batch
		}(),
		"document with an injected response validator": func() yagocrawlcontract.IngestBatch {
			batch := documentIngestBatch()
			batch.ResponseValidators = &yagocrawlcontract.HTTPValidators{
				ETag: "\"v1\"\r\nSet-Cookie: x=1",
			}
			return batch
		}(),
	}
	for name, batch := range cases {
		t.Run(name, func(t *testing.T) {
			out := make(chan crawlresults.IngestDelivery, 1)
			server := newExchangeServer(memQueue(t), out)
			msg := ingestMessageForBatch(t, batch)
			authorizeIngestMessage(t, server, msg, "ingest-invalid-payload")

			_, err := server.SubmitIngest(boundedSubmissionContext(t), msg)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("submit ingest error = %v, want InvalidArgument", err)
			}
			if len(out) != 0 {
				t.Fatal("invalid payload reached the ingest pipeline")
			}
		})
	}
}

func TestSubmitIngestDeliversDocumentWithResponseValidatorsUnchanged(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	server := newExchangeServer(memQueue(t), out)
	batch := documentIngestBatch()
	batch.ResponseValidators = &yagocrawlcontract.HTTPValidators{
		ETag:         `W/"v1-<&>"`,
		LastModified: "Tue, 03 Nov 2026 08:49:37 GMT",
	}
	msg := ingestMessageForBatch(t, batch)
	authorizeIngestMessage(t, server, msg, "ingest-validated-document")
	delivered := make(chan *yagocrawlcontract.HTTPValidators, 1)
	go func() {
		delivery := <-out
		delivered <- delivery.Batch.ResponseValidators
		_ = delivery.Ack(context.Background())
	}()

	ack, err := server.SubmitIngest(context.Background(), msg)
	if err != nil {
		t.Fatalf("submit ingest: %v", err)
	}
	if ack.GetRejected() || ack.GetNotModifiedRecorded() {
		t.Fatalf("document acknowledgement = %+v", ack)
	}
	if got := <-delivered; got == nil || *got != *batch.ResponseValidators {
		t.Fatalf("delivered response validators = %+v, want %+v", got, batch.ResponseValidators)
	}
}

func boundedSubmissionContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	t.Cleanup(cancel)

	return ctx
}

func notModifiedIngestBatch() yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/unchanged",
		Provenance:    []byte("admin"),
		ObservationID: "observation-unchanged",
		NotModified: &yagocrawlcontract.NotModifiedObservation{
			Revision: yagocrawlcontract.DocumentRevision{
				ContentHash:          "content-hash",
				ExtractionGeneration: 1,
				FetchedAt:            time.Date(2026, 11, 3, 8, 49, 37, 0, time.UTC),
			},
		},
	}
}

func documentIngestBatch() yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/changed",
		Provenance:    []byte("admin"),
		ObservationID: "observation-changed",
		Document:      yagocrawlcontract.DocumentIngest{Title: "Changed page"},
	}
}

func ingestMessageForBatch(
	t *testing.T,
	batch yagocrawlcontract.IngestBatch,
) *crawlrpc.IngestBatchMessage {
	t.Helper()
	data, err := yagocrawlcontract.MarshalIngestBatch(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	return &crawlrpc.IngestBatchMessage{BatchJson: data}
}
