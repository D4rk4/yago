package crawlbroker

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

// A page the node's content-quality gate refuses is consumed, not failed, and
// the acknowledgement has to say so: the crawler derives its indexed tally from
// this reply, so an ack indistinguishable from a stored page made every upstream
// counter report an unstored page as indexed.
func TestSubmitIngestReportsAQualityRejection(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	server := newExchangeServer(memQueue(t), out)
	msg := ingestMessage(t, "https://example.org/thin")
	authorizeIngestMessage(t, server, msg, "ingest-thin")
	go func() {
		delivery := <-out
		_ = delivery.Reject(context.Background(), "too-few-words")
	}()

	ack, err := server.SubmitIngest(context.Background(), msg)
	if err != nil {
		t.Fatalf("submit ingest: %v", err)
	}
	if !ack.GetRejected() || ack.GetRejectionRule() != "too-few-words" {
		t.Fatalf(
			"ack rejected=%v rule=%q, want the refusal reported",
			ack.GetRejected(),
			ack.GetRejectionRule(),
		)
	}
}

// A stored page still acknowledges as stored, so a crawler keeps counting it as
// indexed exactly as before.
func TestSubmitIngestReportsAStoredPageAsAccepted(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	server := newExchangeServer(memQueue(t), out)
	msg := ingestMessage(t, "https://example.org/stored")
	authorizeIngestMessage(t, server, msg, "ingest-stored")
	go func() {
		delivery := <-out
		_ = delivery.Ack(context.Background())
	}()

	ack, err := server.SubmitIngest(context.Background(), msg)
	if err != nil {
		t.Fatalf("submit ingest: %v", err)
	}
	if ack.GetRejected() || ack.GetRejectionRule() != "" {
		t.Fatalf("stored page reported as rejected: %+v", ack)
	}
}
