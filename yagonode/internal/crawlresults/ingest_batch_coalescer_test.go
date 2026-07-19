package crawlresults

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestIngestMicroBatchPolicy(t *testing.T) {
	if ingestMicroBatch != 64 || ingestMicroBatchMaximumWait != 10*time.Millisecond ||
		ingestMicroBatchMaximumJSONBytes != 64<<20 {
		t.Fatalf("batch policy = %d/%v/%d, want 64/10ms/64MiB",
			ingestMicroBatch, ingestMicroBatchMaximumWait,
			ingestMicroBatchMaximumJSONBytes)
	}
}

func TestCollectIngestMicroBatchPreservesOrderAndStopsAtCap(t *testing.T) {
	receive := make(chan IngestDelivery, ingestMicroBatch+1)
	for position := 1; position < ingestMicroBatch+2; position++ {
		delivery := IngestDelivery{}
		delivery.Batch.SourceURL = fmt.Sprint(position)
		receive <- delivery
	}
	first := IngestDelivery{}
	first.Batch.SourceURL = "0"
	deadlineCalled := false
	group := collectIngestMicroBatch(
		t.Context(),
		receive,
		first,
		func(time.Duration) <-chan time.Time {
			deadlineCalled = true

			return make(chan time.Time)
		},
	)
	if len(group) != ingestMicroBatch || deadlineCalled {
		t.Fatalf("batch = %d, deadline called = %t", len(group), deadlineCalled)
	}
	for position, delivery := range group {
		if delivery.Batch.SourceURL != fmt.Sprint(position) {
			t.Fatalf("batch[%d] = %q", position, delivery.Batch.SourceURL)
		}
	}
	if len(receive) != 2 {
		t.Fatalf("pending deliveries = %d, want 2", len(receive))
	}
}

func TestCollectIngestMicroBatchUsesBoundedWindow(t *testing.T) {
	receive := make(chan IngestDelivery)
	expires := make(chan time.Time)
	requested := time.Duration(0)
	done := make(chan []IngestDelivery, 1)
	go func() {
		done <- collectIngestMicroBatch(
			t.Context(),
			receive,
			IngestDelivery{},
			func(wait time.Duration) <-chan time.Time {
				requested = wait

				return expires
			},
		)
	}()
	receive <- IngestDelivery{}
	select {
	case <-done:
		t.Fatal("coalescer returned before its bounded window elapsed")
	default:
	}
	expires <- time.Now()
	group := <-done
	if requested != ingestMicroBatchMaximumWait || len(group) != 2 {
		t.Fatalf("wait = %v, batch = %d", requested, len(group))
	}
}

func TestCollectIngestMicroBatchStopsBeforeEncodedPayloadCanExceedLimit(t *testing.T) {
	delivery := IngestDelivery{BatchJSONSize: yagocrawlcontract.MaximumIngestBatchBytes}
	receive := make(chan IngestDelivery, ingestMicroBatch)
	for range ingestMicroBatch {
		receive <- delivery
	}
	group := collectIngestMicroBatch(
		t.Context(),
		receive,
		delivery,
		func(time.Duration) <-chan time.Time { return make(chan time.Time) },
	)
	encodedBytes := 0
	for _, retained := range group {
		encodedBytes += retained.BatchJSONSize
	}
	if encodedBytes > ingestMicroBatchMaximumJSONBytes {
		t.Fatalf("encoded batch bytes = %d, limit %d",
			encodedBytes, ingestMicroBatchMaximumJSONBytes)
	}
	expectedDeliveries := (ingestMicroBatchJSONStopAt +
		yagocrawlcontract.MaximumIngestBatchBytes - 1) /
		yagocrawlcontract.MaximumIngestBatchBytes
	if len(group) != expectedDeliveries || len(receive) != ingestMicroBatch+1-expectedDeliveries {
		t.Fatalf("group = %d, pending = %d, want %d/%d",
			len(group), len(receive), expectedDeliveries,
			ingestMicroBatch+1-expectedDeliveries)
	}
}

func TestCollectIngestMicroBatchTimedPathStopsAtEncodedThreshold(t *testing.T) {
	delivery := IngestDelivery{BatchJSONSize: yagocrawlcontract.MaximumIngestBatchBytes}
	expectedDeliveries := (ingestMicroBatchJSONStopAt +
		yagocrawlcontract.MaximumIngestBatchBytes - 1) /
		yagocrawlcontract.MaximumIngestBatchBytes
	receive := make(chan IngestDelivery)
	windowStarted := make(chan struct{})
	done := make(chan []IngestDelivery, 1)
	go func() {
		done <- collectIngestMicroBatch(
			t.Context(),
			receive,
			delivery,
			func(time.Duration) <-chan time.Time {
				close(windowStarted)

				return make(chan time.Time)
			},
		)
	}()
	<-windowStarted
	for range expectedDeliveries - 1 {
		receive <- delivery
	}
	group := <-done
	if len(group) != expectedDeliveries {
		t.Fatalf("timed group = %d, want %d", len(group), expectedDeliveries)
	}
}

func TestCollectIngestMicroBatchReturnsFirstDeliveryAtEncodedThreshold(t *testing.T) {
	deadlineCalled := false
	group := collectIngestMicroBatch(
		t.Context(),
		make(chan IngestDelivery),
		IngestDelivery{BatchJSONSize: ingestMicroBatchJSONStopAt},
		func(time.Duration) <-chan time.Time {
			deadlineCalled = true

			return make(chan time.Time)
		},
	)
	if len(group) != 1 || deadlineCalled {
		t.Fatalf("group = %d, deadline called = %t, want 1/false",
			len(group), deadlineCalled)
	}
}

func TestCollectIngestMicroBatchStopsOnCancellation(t *testing.T) {
	receive := make(chan IngestDelivery)
	expires := make(chan time.Time)
	windowStarted := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan []IngestDelivery, 1)
	go func() {
		done <- collectIngestMicroBatch(
			ctx,
			receive,
			IngestDelivery{},
			func(time.Duration) <-chan time.Time {
				close(windowStarted)

				return expires
			},
		)
	}()
	<-windowStarted
	cancel()
	if group := <-done; len(group) != 1 {
		t.Fatalf("cancelled batch = %d, want 1", len(group))
	}
}

func TestCollectIngestMicroBatchReturnsImmediatelyForCancelledWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	deadlineCalled := false
	deadline := func(time.Duration) <-chan time.Time {
		deadlineCalled = true

		return make(chan time.Time)
	}
	if group := collectIngestMicroBatch(
		ctx, make(chan IngestDelivery), IngestDelivery{}, deadline,
	); len(group) != 1 || deadlineCalled {
		t.Fatalf("cancelled batch = %d, deadline called = %t", len(group), deadlineCalled)
	}
}

func TestCollectIngestMicroBatchWindowCanFillOrObserveStreamClose(t *testing.T) {
	for _, test := range []struct {
		name        string
		closeStream bool
		want        int
	}{
		{name: "fills", want: ingestMicroBatch},
		{name: "closes", closeStream: true, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			receive := make(chan IngestDelivery)
			expires := make(chan time.Time)
			windowStarted := make(chan struct{})
			done := make(chan []IngestDelivery, 1)
			go func() {
				done <- collectIngestMicroBatch(
					t.Context(),
					receive,
					IngestDelivery{},
					func(time.Duration) <-chan time.Time {
						close(windowStarted)

						return expires
					},
				)
			}()
			<-windowStarted
			if test.closeStream {
				close(receive)
			} else {
				for range ingestMicroBatch - 1 {
					receive <- IngestDelivery{}
				}
			}
			if group := <-done; len(group) != test.want {
				t.Fatalf("batch = %d, want %d", len(group), test.want)
			}
		})
	}
}

func TestCollectIngestMicroBatchReturnsOnClosedStream(t *testing.T) {
	closed := make(chan IngestDelivery)
	close(closed)
	if group := collectIngestMicroBatch(
		t.Context(), closed, IngestDelivery{}, time.After,
	); len(group) != 1 {
		t.Fatalf("closed batch = %d, want 1", len(group))
	}
}

func BenchmarkCollectIngestMicroBatchImmediate(b *testing.B) {
	receive := make(chan IngestDelivery, ingestMicroBatch-1)
	for b.Loop() {
		for range ingestMicroBatch - 1 {
			receive <- IngestDelivery{}
		}
		group := collectIngestMicroBatch(
			context.Background(),
			receive,
			IngestDelivery{},
			time.After,
		)
		if len(group) != ingestMicroBatch {
			b.Fatalf("batch = %d", len(group))
		}
	}
	b.ReportMetric(ingestMicroBatch, "deliveries/batch")
}
