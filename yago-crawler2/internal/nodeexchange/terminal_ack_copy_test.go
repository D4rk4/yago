package nodeexchange

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type terminalAcknowledgementClientFixture struct {
	crawlrpc.CrawlExchangeClient
	requests []*crawlrpc.OrderAck
}

func (f *terminalAcknowledgementClientFixture) AckOrder(
	_ context.Context,
	request *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	f.requests = append(f.requests, request)
	if len(f.requests) == 1 {
		return &crawlrpc.OrderAckResult{ConfirmationToken: []byte("confirmation")}, nil
	}
	return &crawlrpc.OrderAckResult{}, nil
}

func TestAcknowledgeTerminalClonesConfirmationRequest(t *testing.T) {
	client := &terminalAcknowledgementClientFixture{}
	exchange := Exchange{
		client:    client,
		workerID:  "worker-a",
		sessionID: "session-a",
	}
	if err := exchange.AcknowledgeTerminal(
		t.Context(),
		"lease-a",
		[]byte("synthetic order"),
		yagocrawlcontract.CrawlRunTally{},
		crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED,
	); err != nil {
		t.Fatalf("acknowledge terminal order: %v", err)
	}
	if len(client.requests) != 2 || client.requests[0] == client.requests[1] {
		t.Fatalf(
			"acknowledgment requests = %d, cloned=%t",
			len(client.requests),
			len(client.requests) == 2 && client.requests[0] != client.requests[1],
		)
	}
	if len(client.requests[0].GetConfirmationToken()) != 0 {
		t.Fatal("initial acknowledgment gained a confirmation token")
	}
	if string(client.requests[1].GetConfirmationToken()) != "confirmation" {
		t.Fatalf("confirmation token = %q", client.requests[1].GetConfirmationToken())
	}
}
