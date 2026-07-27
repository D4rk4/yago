package ingest

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
)

// TestGRPCIngestPublisherKeepsAPermanentFailureDistinct pins which refusal a
// permanent node error is. Four outcomes leave Publish through the same submit
// loop — a lost lease, a retryable saturation, a cancelled context, and a
// permanent failure — so "an error came back" cannot tell them apart, and each
// one costs the crawl something different. Reading a node-side Internal as a
// lease loss revokes the worker's grant and abandons the job instead of
// recording one failed delivery; reading it as saturation retries a call the
// node will never accept; reading it as a content-quality rejection would file
// the page as fetched-but-refused, which is not a delivery failure at all.
func TestGRPCIngestPublisherKeepsAPermanentFailureDistinct(t *testing.T) {
	registry := crawllease.NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	registry.Renew(time.Now(), time.Hour, []string{"lease"}, []string{"lease"})
	client := &fakeSubmitter{responses: []error{status.Error(codes.Internal, "boom")}}
	publisher := NewGRPCIngestPublisher(
		client,
		WithIngestLeaseSession("worker", "session", registry),
	)
	publisher.retryWait = time.Millisecond

	err := publisher.Publish(crawllease.WithLeaseID(t.Context(), "lease"), testBatch())

	if err == nil {
		t.Fatal("a permanent submit failure must surface")
	}
	if errors.Is(err, crawllease.ErrLeaseLost) {
		t.Fatalf("permanent failure classified as a lost lease: %v", err)
	}
	rejected := new(RejectedError)
	if errors.As(err, &rejected) {
		t.Fatalf("permanent failure classified as a node rejection: %v", err)
	}
	if code := status.Code(err); code != codes.Internal {
		t.Fatalf("status code = %s, want %s", code, codes.Internal)
	}
	if client.calls != 1 {
		t.Fatalf("submit calls = %d, want one (a permanent failure is not retried)", client.calls)
	}
	if !registry.Confirmed("lease") {
		t.Fatal("a permanent submit failure must not revoke the worker's lease grant")
	}
}
