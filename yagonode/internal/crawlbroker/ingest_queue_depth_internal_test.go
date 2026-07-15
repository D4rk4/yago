package crawlbroker

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestIngestReceiverTracksWaitingAndActiveDelivery(t *testing.T) {
	receiver := newIngestReceiver()
	server := newExchangeServer(nil, receiver.out)
	server.beginIngest = receiver.beginIngest
	data, err := yagocrawlcontract.MarshalIngestBatch(yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/page",
	})
	if err != nil {
		t.Fatalf("marshal ingest: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, submitErr := server.SubmitIngest(context.Background(), &crawlrpc.IngestBatchMessage{
			BatchJson: data,
		})
		done <- submitErr
	}()

	waitForIngestDepth(t, receiver, 1)
	delivery := <-receiver.Receive()
	if receiver.Outstanding() != 1 {
		t.Fatalf("active depth = %d, want 1", receiver.Outstanding())
	}
	if err := delivery.Ack(t.Context()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("submit: %v", err)
	}
	if receiver.Outstanding() != 0 {
		t.Fatalf("settled depth = %d, want 0", receiver.Outstanding())
	}
}

func TestIngestReceiverReleasesCancelledWaitingDelivery(t *testing.T) {
	receiver := newIngestReceiver()
	server := newExchangeServer(nil, receiver.out)
	server.beginIngest = receiver.beginIngest
	data, err := yagocrawlcontract.MarshalIngestBatch(yagocrawlcontract.IngestBatch{
		SourceURL: "https://example.org/page",
	})
	if err != nil {
		t.Fatalf("marshal ingest: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, submitErr := server.SubmitIngest(ctx, &crawlrpc.IngestBatchMessage{BatchJson: data})
		done <- submitErr
	}()

	waitForIngestDepth(t, receiver, 1)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("submit must report cancellation")
	}
	if receiver.Outstanding() != 0 {
		t.Fatalf("cancelled depth = %d, want 0", receiver.Outstanding())
	}
}

func waitForIngestDepth(t *testing.T, receiver *IngestReceiver, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for receiver.Outstanding() != want {
		if time.Now().After(deadline) {
			t.Fatalf("depth = %d, want %d", receiver.Outstanding(), want)
		}
		runtime.Gosched()
	}
}
