package ingest

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"testing/iotest"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type fakeSubmitter struct {
	responses []error
	index     int
	calls     int
	deadlines []bool
	messages  []*crawlrpc.IngestBatchMessage
}

func (f *fakeSubmitter) SubmitIngest(
	ctx context.Context,
	message *crawlrpc.IngestBatchMessage,
	_ ...grpc.CallOption,
) (*crawlrpc.IngestAck, error) {
	f.calls++
	f.messages = append(f.messages, message)
	_, deadline := ctx.Deadline()
	f.deadlines = append(f.deadlines, deadline)
	var err error
	if f.index < len(f.responses) {
		err = f.responses[f.index]
		f.index++
	}
	if err != nil {
		return nil, err
	}

	return &crawlrpc.IngestAck{}, nil
}

func testBatch() yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{SourceURL: "https://example.org/a"}
}

func TestGRPCIngestPublisherSubmitsBatch(t *testing.T) {
	client := &fakeSubmitter{}
	publisher := NewGRPCIngestPublisher(client)
	if err := publisher.Publish(context.Background(), testBatch()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("calls = %d, want 1", client.calls)
	}
	if client.deadlines[0] {
		t.Fatal("publisher added an ambiguous per-attempt deadline")
	}
}

func TestGRPCIngestPublisherRejectsInvalidBatchBeforeSubmit(t *testing.T) {
	client := &fakeSubmitter{}
	publisher := NewGRPCIngestPublisher(client)
	err := publisher.Publish(context.Background(), IngestBatch{
		Document: yagocrawlcontract.DocumentIngest{DateConfidence: math.NaN()},
	})
	if err == nil {
		t.Fatal("expected invalid batch error")
	}
	if client.calls != 0 {
		t.Fatalf("calls = %d, want 0", client.calls)
	}
}

func TestGRPCIngestPublisherRetriesOnSaturation(t *testing.T) {
	client := &fakeSubmitter{responses: []error{
		status.Error(codes.Unavailable, "pipeline full"),
		nil,
	}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Millisecond}
	if err := publisher.Publish(context.Background(), testBatch()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want 2", client.calls)
	}
}

func TestGRPCIngestPublisherReturnsNonRetryableError(t *testing.T) {
	client := &fakeSubmitter{responses: []error{status.Error(codes.Internal, "boom")}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Millisecond}
	if err := publisher.Publish(context.Background(), testBatch()); err == nil {
		t.Fatal("expected non-retryable error to surface")
	}
}

func TestGRPCIngestPublisherRetriesLegacySaturationCode(t *testing.T) {
	client := &fakeSubmitter{responses: []error{
		status.Error(codes.ResourceExhausted, "legacy pipeline full"),
		nil,
	}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Millisecond}
	if err := publisher.Publish(context.Background(), testBatch()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want two attempts", client.calls)
	}
}

func TestGRPCIngestPublisherHonorsContext(t *testing.T) {
	client := &fakeSubmitter{responses: []error{status.Error(codes.Unavailable, "full")}}
	publisher := &GRPCIngestPublisher{client: client, retryWait: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, testBatch()); err == nil {
		t.Fatal("expected cancellation error while retrying")
	}
}

func TestGRPCIngestPublisherFencesLeaseSession(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{"lease"}, []string{"lease"})
	client := &fakeSubmitter{responses: []error{
		status.Error(codes.FailedPrecondition, "lease superseded"),
	}}
	publisher := NewGRPCIngestPublisher(
		client,
		WithIngestLeaseSession("worker", "session", registry),
	)
	ctx := crawllease.WithLeaseID(t.Context(), "lease")
	err := publisher.Publish(ctx, testBatch())
	if !errors.Is(err, crawllease.ErrLeaseLost) {
		t.Fatalf("publish error = %v, want lease lost", err)
	}
	if client.calls != 1 || len(client.messages) != 1 {
		t.Fatalf("submit calls = %d/%d, want one", client.calls, len(client.messages))
	}
	message := client.messages[0]
	if message.GetLeaseId() != "lease" || message.GetWorkerId() != "worker" ||
		message.GetWorkerSessionId() != "session" {
		t.Fatalf("lease identity = %+v", message)
	}
	if registry.Confirmed("lease") {
		t.Fatal("failed-precondition ingest retained lease grant")
	}
}

func TestGRPCIngestPublisherRejectsMissingLeaseIdentity(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	client := &fakeSubmitter{}
	publisher := NewGRPCIngestPublisher(
		client,
		WithIngestLeaseSession("worker", "session", registry),
	)
	if err := publisher.Publish(
		t.Context(),
		testBatch(),
	); !errors.Is(
		err,
		crawllease.ErrLeaseLost,
	) {
		t.Fatalf("publish error = %v, want lease lost", err)
	}
	if client.calls != 0 {
		t.Fatalf("missing lease submitted %d times", client.calls)
	}
}

func TestJitteredIngestRetryWait(t *testing.T) {
	wait := 100 * time.Millisecond
	if got := jitteredIngestRetryWait(wait, bytes.NewReader(make([]byte, 8))); got != wait/2 {
		t.Fatalf("zero entropy wait = %s, want %s", got, wait/2)
	}
	if got := jitteredIngestRetryWait(
		wait,
		iotest.ErrReader(errors.New("entropy unavailable")),
	); got != wait/2 {
		t.Fatalf("fallback wait = %s, want %s", got, wait/2)
	}
}
