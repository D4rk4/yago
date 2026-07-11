package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type documentCommitReceiver struct {
	canonical []documentstore.Document
	err       error
}

func (*documentCommitReceiver) Receive(
	context.Context,
	[]documentstore.Document,
) (documentstore.Receipt, error) {
	return documentstore.Receipt{}, nil
}

func (r *documentCommitReceiver) CanonicalDocuments(
	context.Context,
	[]documentstore.Document,
) ([]documentstore.Document, error) {
	return r.canonical, r.err
}

type documentReceiverWithoutCanonicalization struct{}

func (documentReceiverWithoutCanonicalization) Receive(
	context.Context,
	[]documentstore.Document,
) (documentstore.Receipt, error) {
	return documentstore.Receipt{}, nil
}

func TestDocumentCommitSelection(t *testing.T) {
	fallback := []documentstore.Document{{Title: "fallback"}}
	canonical := []documentstore.Document{{Title: "canonical"}}
	consumer := &IngestConsumer{documents: documentReceiverWithoutCanonicalization{}}
	got, err := consumer.canonicalDocuments(t.Context(), fallback)
	if err != nil || got[0].Title != "fallback" {
		t.Fatalf("fallback canonicalization = %#v, %v", got, err)
	}
	got = consumer.committedDocuments(documentstore.Receipt{}, fallback)
	if got[0].Title != "fallback" {
		t.Fatalf("fallback commit = %#v", got)
	}

	consumer.documents = &documentCommitReceiver{canonical: canonical}
	got, err = consumer.canonicalDocuments(t.Context(), fallback)
	if err != nil || got[0].Title != "canonical" {
		t.Fatalf("canonical documents = %#v, %v", got, err)
	}
	if got := consumer.committedDocuments(documentstore.Receipt{}, fallback); got != nil {
		t.Fatalf("empty canonical commit = %#v", got)
	}
	got = consumer.committedDocuments(documentstore.Receipt{
		CommittedDocuments: []documentstore.Document{{Title: "committed"}},
	}, fallback)
	if got[0].Title != "committed" {
		t.Fatalf("committed documents = %#v", got)
	}
}

func TestCanonicalDocumentFailure(t *testing.T) {
	sentinel := errors.New("canonical failure")
	consumer := &IngestConsumer{documents: &documentCommitReceiver{err: sentinel}}
	if _, err := consumer.canonicalDocuments(t.Context(), []documentstore.Document{{}}); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("canonical failure = %v", err)
	}
}

func TestCanonicalDocumentFailureRedeliversSingleAndGroup(t *testing.T) {
	sentinel := errors.New("canonical failure")
	consumer := &IngestConsumer{
		documents: &documentCommitReceiver{err: sentinel},
		observer:  noopIngestObserver{},
	}
	naks := 0
	delivery := IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://example.org/",
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://example.org/",
			},
		},
		Nak: func(context.Context) error {
			naks++

			return nil
		},
	}
	if !consumer.storeDocument(t.Context(), delivery, delivery.Batch) || naks != 1 {
		t.Fatalf("single canonical failure naks = %d", naks)
	}
	if consumer.storeDocumentGroup(
		t.Context(),
		[]IngestDelivery{delivery},
		[]documentstore.Document{{NormalizedURL: "https://example.org/"}},
	) || naks != 2 {
		t.Fatalf("group canonical failure naks = %d", naks)
	}
}
