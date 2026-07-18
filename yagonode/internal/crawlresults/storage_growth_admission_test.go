package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type fixedGrowthAdmission struct {
	err   error
	calls int
}

func (a *fixedGrowthAdmission) CheckGrowth() error {
	a.calls++

	return a.err
}

func TestStorageGrowthAdmissionAllowsEmptyAndHealthyDeliveries(t *testing.T) {
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	if !consumer.admitStorageGrowth(t.Context(), nil) {
		t.Fatal("empty delivery group rejected")
	}
	admission := &fixedGrowthAdmission{}
	consumer.AdmitGrowth(admission)
	if !consumer.admitStorageGrowth(t.Context(), []IngestDelivery{{}}) || admission.calls != 1 {
		t.Fatalf("healthy admission calls = %d", admission.calls)
	}
}

func TestStoragePressureRedeliversGrowthButPurgesRemoval(t *testing.T) {
	pressure := &fixedGrowthAdmission{err: errors.New("pressure")}
	purger := &countingPurger{}
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	consumer.AdmitGrowth(pressure)
	consumer.PurgeURLs(purger)
	regularAcknowledged := 0
	regularRedelivered := 0
	removalAcknowledged := 0
	consumer.absorbGroup(t.Context(), []IngestDelivery{
		{
			Batch: yagocrawlcontract.IngestBatch{SourceURL: "https://a.example/page"},
			Ack: func(context.Context) error {
				regularAcknowledged++

				return nil
			},
			Nak: func(context.Context) error {
				regularRedelivered++

				return nil
			},
		},
		{
			Batch: yagocrawlcontract.IngestBatch{
				SourceURL: "https://a.example/gone", Removed: true,
			},
			Ack: func(context.Context) error {
				removalAcknowledged++

				return nil
			},
			Nak: func(context.Context) error { return nil },
		},
	})
	if regularAcknowledged != 0 || regularRedelivered != 1 ||
		removalAcknowledged != 1 || purger.calls != 1 || pressure.calls != 1 {
		t.Fatalf(
			"growth/removal settlements = ack:%d nak:%d removal:%d purge:%d checks:%d",
			regularAcknowledged,
			regularRedelivered,
			removalAcknowledged,
			purger.calls,
			pressure.calls,
		)
	}
}

func TestStoragePressureRedeliversSingleDelivery(t *testing.T) {
	pressure := &fixedGrowthAdmission{err: errors.New("pressure")}
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	consumer.AdmitGrowth(pressure)
	redelivered := 0
	consumer.absorb(t.Context(), IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{SourceURL: "https://a.example/page"},
		Nak: func(context.Context) error {
			redelivered++

			return nil
		},
	})
	if redelivered != 1 || pressure.calls != 1 {
		t.Fatalf("single delivery redeliveries=%d checks=%d", redelivered, pressure.calls)
	}
}
