package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type acknowledgingSubmitter struct {
	acknowledgement *crawlrpc.IngestAck
	messages        []*crawlrpc.IngestBatchMessage
}

func (s *acknowledgingSubmitter) SubmitIngest(
	_ context.Context,
	message *crawlrpc.IngestBatchMessage,
	_ ...grpc.CallOption,
) (*crawlrpc.IngestAck, error) {
	s.messages = append(s.messages, message)

	return s.acknowledgement, nil
}

func cachedDocumentObservation() yagocrawlcontract.NotModifiedObservation {
	return yagocrawlcontract.NotModifiedObservation{
		Revision: yagocrawlcontract.DocumentRevision{
			ContentHash:          "content-hash",
			ExtractionGeneration: 2,
			FetchedAt:            time.Date(2026, 11, 2, 7, 30, 0, 0, time.UTC),
		},
	}
}

func unchangedPageEnvelope() Envelope {
	return Envelope{
		SourceURL:     "https://example.org/unchanged",
		Provenance:    []byte("run"),
		ProfileHandle: "handle",
		ObservationID: "observation-unchanged",
		ObservedAt:    time.Date(2026, 11, 3, 9, 0, 0, 0, time.FixedZone("test", 3600)),
		ResponseValidators: &yagocrawlcontract.HTTPValidators{
			ETag:         `W/"v2-<&>"`,
			LastModified: "Mon, 02 Nov 2026 07:30:00 GMT",
		},
	}
}

func TestNotModifiedBatchCarriesObservationAndIdentity(t *testing.T) {
	envelope := unchangedPageEnvelope()
	batch := NewNotModifiedBatch(cachedDocumentObservation(), envelope)

	if batch.NotModified == nil || *batch.NotModified != cachedDocumentObservation() {
		t.Fatalf("not-modified observation = %+v", batch.NotModified)
	}
	if batch.SourceURL != envelope.SourceURL || string(batch.Provenance) != "run" ||
		batch.ProfileHandle != envelope.ProfileHandle {
		t.Fatalf("not-modified source = %+v", batch)
	}
	if batch.ObservationID == "" || batch.ObservationID == envelope.ObservationID {
		t.Fatalf("not-modified observation ID = %q, want its own identity", batch.ObservationID)
	}
	precedingInstant := envelope.ObservedAt.Add(-time.Nanosecond)
	if !batch.ObservedAt.Equal(precedingInstant) || batch.ObservedAt.Location() != time.UTC {
		t.Fatalf("observed at = %v, want %v in UTC", batch.ObservedAt, precedingInstant)
	}
	if batch.ResponseValidators != envelope.ResponseValidators {
		t.Fatalf("response validators = %+v", batch.ResponseValidators)
	}
	if err := batch.ValidatePayload(); err != nil {
		t.Fatalf("constructed not-modified batch is invalid: %v", err)
	}
}

func TestNotModifiedBatchAssignsMissingObservationIdentity(t *testing.T) {
	envelope := unchangedPageEnvelope()
	envelope.ObservationID = ""
	envelope.ObservedAt = time.Time{}

	batch := NewNotModifiedBatch(cachedDocumentObservation(), envelope)

	if batch.ObservationID == "" || batch.ObservedAt.IsZero() {
		t.Fatalf(
			"not-modified observation identity = %q at %v",
			batch.ObservationID,
			batch.ObservedAt,
		)
	}
}

func TestPublisherReportsUnrecordedNotModifiedObservation(t *testing.T) {
	acknowledgements := map[string]*crawlrpc.IngestAck{
		"node without conditional recrawl":  {},
		"node gate refused the observation": {Rejected: true, RejectionRule: "too-few-words"},
	}
	for name, acknowledgement := range acknowledgements {
		t.Run(name, func(t *testing.T) {
			publisher := NewGRPCIngestPublisher(
				&acknowledgingSubmitter{acknowledgement: acknowledgement},
			)

			err := publisher.Publish(
				context.Background(),
				NewNotModifiedBatch(cachedDocumentObservation(), unchangedPageEnvelope()),
			)

			if !errors.Is(err, ErrNotModifiedUnrecorded) {
				t.Fatalf("publish error = %v, want ErrNotModifiedUnrecorded", err)
			}
		})
	}
}

func TestPublisherAcceptsRecordedNotModifiedObservationUnchanged(t *testing.T) {
	client := &acknowledgingSubmitter{
		acknowledgement: &crawlrpc.IngestAck{NotModifiedRecorded: true},
	}
	batch := NewNotModifiedBatch(cachedDocumentObservation(), unchangedPageEnvelope())

	if err := NewGRPCIngestPublisher(client).Publish(context.Background(), batch); err != nil {
		t.Fatalf("publish recorded observation: %v", err)
	}
	if len(client.messages) != 1 {
		t.Fatalf("submissions = %d, want 1", len(client.messages))
	}
	sent, err := yagocrawlcontract.UnmarshalIngestBatch(client.messages[0].GetBatchJson())
	if err != nil {
		t.Fatalf("decode submitted batch: %v", err)
	}
	if *sent.ResponseValidators != *batch.ResponseValidators ||
		!sent.NotModified.Revision.Matches(batch.NotModified.Revision) {
		t.Fatalf("submitted batch = %+v, want %+v", sent, batch)
	}
}

func TestPublisherKeepsDocumentAcknowledgementWithoutNotModifiedRecord(t *testing.T) {
	client := &acknowledgingSubmitter{acknowledgement: &crawlrpc.IngestAck{}}
	batch := testBatch()
	batch.Document = yagocrawlcontract.DocumentIngest{NormalizedURL: batch.SourceURL}
	batch.ResponseValidators = unchangedPageEnvelope().ResponseValidators

	if err := NewGRPCIngestPublisher(client).Publish(context.Background(), batch); err != nil {
		t.Fatalf("document publish error = %v, want stored", err)
	}
	sent, err := yagocrawlcontract.UnmarshalIngestBatch(client.messages[0].GetBatchJson())
	if err != nil {
		t.Fatalf("decode submitted batch: %v", err)
	}
	if *sent.ResponseValidators != *batch.ResponseValidators {
		t.Fatalf("submitted response validators = %+v", sent.ResponseValidators)
	}
}

func TestPublisherRefusesInvalidPayloadWithoutTruncating(t *testing.T) {
	oversized := NewNotModifiedBatch(cachedDocumentObservation(), unchangedPageEnvelope())
	oversized.ResponseValidators = &yagocrawlcontract.HTTPValidators{
		ETag: `"` + strings.Repeat("v", yagocrawlcontract.MaximumHTTPValidatorBytes) + `"`,
	}
	mixed := NewNotModifiedBatch(cachedDocumentObservation(), unchangedPageEnvelope())
	mixed.Removed = true
	for name, batch := range map[string]IngestBatch{
		"oversized response validator": oversized,
		"removal beside not-modified":  mixed,
	} {
		t.Run(name, func(t *testing.T) {
			client := &acknowledgingSubmitter{
				acknowledgement: &crawlrpc.IngestAck{NotModifiedRecorded: true},
			}

			err := NewGRPCIngestPublisher(client).Publish(context.Background(), batch)

			if err == nil {
				t.Fatal("invalid payload was published")
			}
			if len(client.messages) != 0 {
				t.Fatalf("submissions = %d, want 0", len(client.messages))
			}
		})
	}
}
