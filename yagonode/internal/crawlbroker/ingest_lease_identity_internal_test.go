package crawlbroker

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type incompleteIngestLeaseIdentity struct {
	name    string
	prepare func(*yagocrawlcontract.IngestBatch, *crawlrpc.IngestBatchMessage)
}

// TestIngestRequiresEveryLeaseIdentityField pins each arm of the ingest lease
// identity guard separately, with the other two arms left valid. The arms are not
// interchangeable and none of them is redundant: authorizedLeaseOrder only
// compares the batch provenance against the leased order's run when RunID is
// non-empty, and only compares profile handles when ProfileHandle is non-empty.
// A batch that omits either field therefore skips the very cross-check that
// proves it belongs to the lease it presents, so the crawler would choose which
// authorization rules apply to it. Asserting the all-empty case alone cannot see
// an arm being dropped, because the remaining arm still refuses that message.
func TestIngestRequiresEveryLeaseIdentityField(t *testing.T) {
	incomplete := []incompleteIngestLeaseIdentity{
		{
			name: "lease",
			prepare: func(_ *yagocrawlcontract.IngestBatch, msg *crawlrpc.IngestBatchMessage) {
				msg.LeaseId = ""
			},
		},
		{
			name: "provenance",
			prepare: func(batch *yagocrawlcontract.IngestBatch, _ *crawlrpc.IngestBatchMessage) {
				batch.Provenance = nil
			},
		},
		{
			name: "profile",
			prepare: func(batch *yagocrawlcontract.IngestBatch, _ *crawlrpc.IngestBatchMessage) {
				batch.ProfileHandle = ""
			},
		},
	}
	for _, identity := range incomplete {
		t.Run(identity.name, func(t *testing.T) {
			ingest := make(chan crawlresults.IngestDelivery, 1)
			server := newExchangeServer(memQueue(t), ingest)
			message := ingestMessage(t, "https://example.test/identity/"+identity.name)
			authorizeIngestMessage(t, server, message, "identity-"+identity.name)
			batch, err := yagocrawlcontract.UnmarshalIngestBatch(message.BatchJson)
			if err != nil {
				t.Fatalf("decode authorized batch: %v", err)
			}
			identity.prepare(&batch, message)
			message.BatchJson, err = yagocrawlcontract.MarshalIngestBatch(batch)
			if err != nil {
				t.Fatalf("encode incomplete batch: %v", err)
			}
			refused := make(chan error, 1)
			go func() {
				_, err := server.SubmitIngest(t.Context(), message)
				refused <- err
			}()
			select {
			case err := <-refused:
				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf("incomplete %s status = %v, want InvalidArgument",
						identity.name, status.Code(err))
				}
			case <-time.After(time.Second):
				t.Fatalf("incomplete %s was admitted for absorption", identity.name)
			}
			if len(ingest) != 0 {
				t.Fatalf("incomplete %s reached the ingest pipeline", identity.name)
			}
		})
	}
}

// TestIngestAdmitsACompleteLeaseIdentity is the accepting half of the guard
// above. Every field the guard demands is present here, so the batch has to reach
// the ingest pipeline; a guard broadened to refuse a legitimate submission would
// stall every crawler while still passing the refusal cases.
func TestIngestAdmitsACompleteLeaseIdentity(t *testing.T) {
	ingest := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(memQueue(t), ingest)
	message := ingestMessage(t, "https://example.test/identity/complete")
	authorizeIngestMessage(t, server, message, "identity-complete")
	submitted := make(chan error, 1)
	go func() {
		_, err := server.SubmitIngest(t.Context(), message)
		submitted <- err
	}()
	var delivery crawlresults.IngestDelivery
	select {
	case delivery = <-ingest:
	case err := <-submitted:
		t.Fatalf("complete ingest identity was refused: %v", err)
	case <-time.After(time.Second):
		t.Fatal("complete ingest identity never reached the ingest pipeline")
	}
	if err := delivery.AuthorizeLeaseSnapshot(t.Context()); err != nil {
		t.Fatalf("authorize complete ingest identity: %v", err)
	}
	if err := delivery.Ack(t.Context()); err != nil {
		t.Fatalf("acknowledge complete ingest identity: %v", err)
	}
	if err := <-submitted; err != nil {
		t.Fatalf("submit complete ingest identity: %v", err)
	}
}
