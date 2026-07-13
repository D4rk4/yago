package crawlresults

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestIngestDeliverySupersedesUsesDatesAndStablePayloadOrder(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	delivery := func(handle string, fetched time.Time, confidence float64) IngestDelivery {
		observationID := handle
		if math.IsNaN(confidence) {
			observationID = ""
		}
		return IngestDelivery{Batch: yagocrawlcontract.IngestBatch{
			SourceURL:     "https://example.org/",
			ProfileHandle: handle,
			ObservationID: observationID,
			Document: yagocrawlcontract.DocumentIngest{
				FetchedAt:      fetched,
				DateConfidence: confidence,
			},
		}}
	}
	tests := []struct {
		name      string
		candidate IngestDelivery
		current   IngestDelivery
		want      bool
	}{
		{"newer", delivery("a", base.Add(time.Hour), 0), delivery("z", base, 0), true},
		{"older", delivery("z", base, 0), delivery("a", base.Add(time.Hour), 0), false},
		{"valid over invalid", delivery("a", base, 0), delivery("z", base, math.NaN()), true},
		{"invalid under valid", delivery("z", base, math.NaN()), delivery("a", base, 0), false},
		{
			"invalid handle order",
			delivery("z", base, math.NaN()),
			delivery("a", base, math.NaN()),
			true,
		},
		{"payload order", delivery("z", base, 0), delivery("a", base, 0), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ingestDeliverySupersedes(test.candidate, test.current); got != test.want {
				t.Fatalf("supersedes = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCoalesceIngestDeliveriesReplacesOlderWinner(t *testing.T) {
	older := IngestDelivery{Batch: yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/",
		Document: yagocrawlcontract.DocumentIngest{
			FetchedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	newer := older
	newer.Batch.Document.FetchedAt = older.Batch.Document.FetchedAt.Add(time.Hour)
	newer.Ack = func(context.Context) error { return nil }
	newer.Nak = func(context.Context) error { return nil }
	older.Ack = func(context.Context) error { return nil }
	older.Nak = func(context.Context) error { return nil }
	got := coalesceIngestDeliveries([]IngestDelivery{older, newer})
	if len(got) != 1 || !got[0].Batch.Document.FetchedAt.Equal(newer.Batch.Document.FetchedAt) {
		t.Fatalf("coalesced = %#v, want newer delivery", got)
	}
}

func TestCoalesceIngestDeliveriesOrdersRemovalAgainstLiveDelivery(t *testing.T) {
	live := IngestDelivery{Batch: yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://example.org/",
			FetchedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	removed := IngestDelivery{Batch: yagocrawlcontract.IngestBatch{
		SourceURL:     "https://example.org/",
		ObservationID: "removed",
		ObservedAt:    live.Batch.Document.FetchedAt.Add(time.Hour),
		Removed:       true,
	}}

	got := coalesceIngestDeliveries([]IngestDelivery{live, removed})
	if len(got) != 1 || !got[0].Batch.Removed {
		t.Fatalf("live then removal = %#v, want removal", got)
	}
	got = coalesceIngestDeliveries([]IngestDelivery{removed, live})
	if len(got) != 1 || !got[0].Batch.Removed {
		t.Fatalf("removal then live = %#v, want newer removal", got)
	}
	live.Batch.ObservationID = "new-live"
	live.Batch.ObservedAt = removed.Batch.ObservedAt.Add(time.Hour)
	got = coalesceIngestDeliveries([]IngestDelivery{removed, live})
	if len(got) != 1 || got[0].Batch.Removed {
		t.Fatalf("removal then newer live = %#v, want live", got)
	}
}
