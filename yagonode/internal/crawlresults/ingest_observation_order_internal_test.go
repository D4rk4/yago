package crawlresults

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type scriptedObservationHistory struct {
	dispositions []observationDisposition
	beginErr     error
	completeErr  error
}

func (h scriptedObservationHistory) Begin(
	context.Context,
	[]yagocrawlcontract.IngestBatch,
) ([]observationDisposition, error) {
	return h.dispositions, h.beginErr
}

func (h scriptedObservationHistory) Complete(
	context.Context,
	[]yagocrawlcontract.IngestBatch,
) error {
	return h.completeErr
}

type observationURLReceiver struct{}

func (observationURLReceiver) Receive(
	context.Context,
	[]yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	return urlmeta.Receipt{}, nil
}

type observationPostingReceiver struct{}

func (observationPostingReceiver) Receive(
	context.Context,
	[]yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	return rwi.Receipt{}, nil
}

func orderedTestDelivery(url string) IngestDelivery {
	return IngestDelivery{Batch: yagocrawlcontract.IngestBatch{
		SourceURL:     url,
		ObservationID: url,
		ObservedAt:    time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC),
	}}
}

func TestObservationCoordinationEdges(t *testing.T) {
	consumer := NewIngestConsumer(nil, nil, observationURLReceiver{}, observationPostingReceiver{})
	if got := consumer.beginObservations(t.Context(), nil); len(got) != 0 {
		t.Fatalf("empty begin = %v", got)
	}
	if !consumer.completeObservations(t.Context(), nil) {
		t.Fatal("empty completion failed")
	}
	consumer.observations = nil
	delivery := orderedTestDelivery("https://example.org/page")
	if got := consumer.beginObservations(t.Context(), []IngestDelivery{delivery}); len(got) != 1 {
		t.Fatalf("nil history begin = %v", got)
	}
	if !consumer.completeObservations(t.Context(), []IngestDelivery{delivery}) {
		t.Fatal("nil history completion failed")
	}

	naked := 0
	delivery.Nak = func(context.Context) error { naked++; return nil }
	consumer.observations = scriptedObservationHistory{beginErr: errors.New("read failed")}
	if got := consumer.beginObservations(t.Context(), []IngestDelivery{delivery}); len(got) != 0 {
		t.Fatalf("failed begin = %v", got)
	}
	if naked != 1 {
		t.Fatalf("begin naks = %d, want 1", naked)
	}

	acked := 0
	delivery.Ack = func(context.Context) error {
		acked++

		return errors.New("ack failed")
	}
	consumer.observations = scriptedObservationHistory{
		dispositions: []observationDisposition{observationDuplicate},
	}
	if got := consumer.beginObservations(t.Context(), []IngestDelivery{delivery}); len(got) != 0 {
		t.Fatalf("duplicate begin = %v", got)
	}
	if acked != 1 {
		t.Fatalf("duplicate acks = %d, want 1", acked)
	}
}

func TestObservationCompletionFailureStopsSettlements(t *testing.T) {
	completionErr := errors.New("completion failed")
	newConsumer := func() *IngestConsumer {
		consumer := NewIngestConsumer(
			nil,
			nil,
			observationURLReceiver{},
			observationPostingReceiver{},
		)
		consumer.observations = scriptedObservationHistory{completeErr: completionErr}

		return consumer
	}

	t.Run("removal", func(t *testing.T) {
		consumer := newConsumer()
		consumer.PurgeURLs(&countingPurger{})
		naked := 0
		delivery := orderedTestDelivery("https://example.org/gone")
		delivery.Batch.Removed = true
		delivery.Nak = func(context.Context) error { naked++; return nil }
		consumer.purgeRemoval(t.Context(), delivery)
		if naked != 1 {
			t.Fatalf("removal naks = %d, want 1", naked)
		}
	})

	t.Run("single", func(t *testing.T) {
		consumer := newConsumer()
		naked := 0
		delivery := orderedTestDelivery("https://example.org/page")
		delivery.Nak = func(context.Context) error { naked++; return nil }
		consumer.absorbTail(t.Context(), delivery)
		if naked != 1 {
			t.Fatalf("single naks = %d, want 1", naked)
		}
	})

	t.Run("group", func(t *testing.T) {
		consumer := newConsumer()
		naked := 0
		group := []IngestDelivery{
			orderedTestDelivery("https://example.org/a"),
			orderedTestDelivery("https://example.org/b"),
		}
		for index := range group {
			group[index].Nak = func(context.Context) error { naked++; return nil }
		}
		consumer.absorbTailGroup(t.Context(), group)
		if naked != 2 {
			t.Fatalf("group naks = %d, want 2", naked)
		}
	})
}
