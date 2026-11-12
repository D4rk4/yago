package nodeexchange

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func (exchange *Exchange) ReadSearchDemand(
	ctx context.Context,
	cursor string,
	pageSize uint32,
) (*crawlrpc.SearchDemandPage, error) {
	requestContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	requestContext = metadata.AppendToOutgoingContext(
		requestContext,
		"authorization",
		"Bearer "+exchange.apiKey,
	)
	page, err := exchange.dataClient.ReadSearchDemand(
		requestContext,
		&crawlrpc.SearchDemandRequest{Cursor: cursor, PageSize: pageSize},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, fmt.Errorf("read node search demand: %w", err)
	}
	return page, nil
}

func (exchange *Exchange) StreamDocumentMutations(
	ctx context.Context,
) (grpc.BidiStreamingClient[crawlrpc.DocumentMutation, crawlrpc.DocumentMutationAck], error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+exchange.apiKey)
	stream, err := exchange.dataClient.StreamDocumentMutations(ctx)
	if err != nil {
		return nil, fmt.Errorf("open node document mutation stream: %w", err)
	}
	return stream, nil
}
