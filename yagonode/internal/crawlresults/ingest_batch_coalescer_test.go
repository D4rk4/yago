package crawlresults

import (
	"context"
	"testing"
	"time"
)

func TestCollectIngestMicroBatchSkipsDeadlineWhenAlreadyFull(t *testing.T) {
	receive := make(chan IngestDelivery, ingestMicroBatch-1)
	for range ingestMicroBatch - 1 {
		receive <- IngestDelivery{}
	}
	deadlineCalled := false
	group := collectIngestMicroBatch(
		t.Context(),
		receive,
		IngestDelivery{},
		func(time.Duration) <-chan time.Time {
			deadlineCalled = true

			return make(chan time.Time)
		},
	)
	if len(group) != ingestMicroBatch || deadlineCalled {
		t.Fatalf("batch = %d, deadline called = %t", len(group), deadlineCalled)
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
	if requested != ingestMicroBatchMaximumWait ||
		requested > 2*time.Millisecond || len(group) != 2 {
		t.Fatalf("wait = %v, batch = %d", requested, len(group))
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
