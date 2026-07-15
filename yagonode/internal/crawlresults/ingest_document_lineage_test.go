package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type failingIngestDocumentLineages struct {
	err      error
	reserves int
}

func (*failingIngestDocumentLineages) Receive(
	context.Context,
	[]documentstore.Document,
) (documentstore.Receipt, error) {
	return documentstore.Receipt{}, nil
}

func (f *failingIngestDocumentLineages) ReserveDocumentLineages(
	context.Context,
	[]string,
) (documentstore.DocumentLineageReservation, error) {
	f.reserves++

	return nil, f.err
}

func (*failingIngestDocumentLineages) ReleaseDocumentLineages(
	documentstore.DocumentLineageReservation,
) {
}

func TestIngestDocumentLineageHelpersReserveCanonicalizeAndRelease(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	consumer := &IngestConsumer{
		documents:         receiver,
		anchors:           receiver.(documentstore.InboundAnchorReceiver),
		lineages:          receiver.(documentstore.DocumentLineageReserver),
		reservedAnchors:   receiver.(documentstore.ReservedOutboundAnchorReceiver),
		reservedDocuments: receiver.(documentstore.ReservedCanonicalDocumentDirectory),
		observer:          noopIngestObserver{},
	}
	sourceURL := "https://source.example/"
	canonicalURL := "https://canonical.example/"
	reservation, err := consumer.reserveIngestDocumentLineages(
		t.Context(),
		[]IngestDelivery{
			{Batch: yagocrawlcontract.IngestBatch{SourceURL: sourceURL}},
			{Batch: yagocrawlcontract.IngestBatch{
				SourceURL: sourceURL,
				Document: yagocrawlcontract.DocumentIngest{
					NormalizedURL: sourceURL,
					CanonicalURL:  canonicalURL,
				},
			}},
		},
	)
	if err != nil || reservation == nil {
		t.Fatalf("reserve ingest lineages = %#v, %v", reservation, err)
	}
	canonical, err := consumer.canonicalIngestDocuments(
		t.Context(),
		reservation,
		[]documentstore.Document{{NormalizedURL: sourceURL}},
	)
	if err != nil || len(canonical) != 1 || canonical[0].NormalizedURL != sourceURL {
		t.Fatalf("canonical ingest documents = %#v, %v", canonical, err)
	}
	assertReservedAnchorRouting(t, consumer, reservation, sourceURL)
	consumer.releaseIngestDocumentLineages(reservation)
	consumer.releaseIngestDocumentLineages(reservation)
	if _, err := consumer.canonicalIngestDocuments(
		t.Context(),
		reservation,
		[]documentstore.Document{{NormalizedURL: sourceURL}},
	); err == nil {
		t.Fatal("released ingest reservation was accepted")
	}
}

func assertReservedAnchorRouting(
	t *testing.T,
	consumer *IngestConsumer,
	reservation documentstore.DocumentLineageReservation,
	sourceURL string,
) {
	t.Helper()
	sets := []documentstore.OutboundAnchorSet{{
		SourceURL: sourceURL,
		Anchors: []documentstore.OutboundAnchor{{
			TargetURL: "https://target.example/",
			Text:      "source evidence",
		}},
	}}
	if deferred := consumer.replaceOutboundAnchors(
		t.Context(),
		[]IngestDelivery{{Batch: yagocrawlcontract.IngestBatch{SourceURL: sourceURL}}},
		sets,
		reservation,
	); deferred {
		t.Fatal("reserved anchor replacement was deferred")
	}
	naked := 0
	missingReservedAnchors := &IngestConsumer{
		anchors:  consumer.anchors,
		observer: noopIngestObserver{},
	}
	if deferred := missingReservedAnchors.replaceOutboundAnchors(
		t.Context(),
		[]IngestDelivery{{
			Batch: yagocrawlcontract.IngestBatch{SourceURL: sourceURL},
			Nak: func(context.Context) error {
				naked++

				return nil
			},
		}},
		sets,
		reservation,
	); !deferred || naked != 1 {
		t.Fatalf("missing reserved anchor receiver = %t, %d", deferred, naked)
	}
}

func TestIngestDocumentLineageHelpersCoverOptionalAndFailurePaths(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	withoutLineages := &IngestConsumer{documents: receiver}
	if reservation, err := withoutLineages.reserveIngestDocumentLineages(
		t.Context(),
		nil,
	); err != nil || reservation != nil {
		t.Fatalf("optional ingest lineages = %#v, %v", reservation, err)
	}
	documents := []documentstore.Document{{NormalizedURL: "https://source.example/"}}
	canonical, err := withoutLineages.canonicalIngestDocuments(t.Context(), nil, documents)
	if err != nil || len(canonical) != 1 {
		t.Fatalf("unreserved canonical documents = %#v, %v", canonical, err)
	}
	withoutLineages.releaseIngestDocumentLineages(nil)

	lineages := receiver.(documentstore.DocumentLineageReserver)
	reservation, err := lineages.ReserveDocumentLineages(
		t.Context(),
		[]string{"https://source.example/"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer lineages.ReleaseDocumentLineages(reservation)
	missingDirectory := &IngestConsumer{lineages: lineages}
	if _, err := missingDirectory.canonicalIngestDocuments(
		t.Context(),
		reservation,
		documents,
	); err == nil {
		t.Fatal("missing reserved document directory was accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (&IngestConsumer{lineages: lineages}).reserveIngestDocumentLineages(
		ctx,
		[]IngestDelivery{{Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://cancel.example/",
		}}},
	); err == nil {
		t.Fatal("cancelled ingest lineage reservation succeeded")
	}
}

func TestIngestDocumentLineageReservationFailureRedeliversSingleAndGroup(t *testing.T) {
	wantErr := errors.New("reservation failed")
	documents := &failingIngestDocumentLineages{err: wantErr}
	consumer := NewIngestConsumer(stubStream{}, documents, nil, nil)
	naked := 0
	delivery := func(url string) IngestDelivery {
		return IngestDelivery{
			Batch: yagocrawlcontract.IngestBatch{
				SourceURL: url,
				Document: yagocrawlcontract.DocumentIngest{
					NormalizedURL: url,
					ExtractedText: "alpha beta gamma delta",
				},
			},
			Ack: func(context.Context) error {
				t.Fatal("reservation failure was acknowledged")

				return nil
			},
			Nak: func(context.Context) error {
				naked++

				return nil
			},
		}
	}
	consumer.absorb(t.Context(), delivery("https://single.example/"))
	consumer.absorbGroup(t.Context(), []IngestDelivery{
		delivery("https://first.example/"),
		delivery("https://second.example/"),
	})
	if documents.reserves != 2 || naked != 3 {
		t.Fatalf("reservation failures = reserves:%d redeliveries:%d", documents.reserves, naked)
	}
}
