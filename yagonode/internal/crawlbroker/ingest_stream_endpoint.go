package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type ingestStreamOutcome struct {
	submissionID   string
	acknowledgment *crawlrpc.IngestAck
	errorCode      codes.Code
}

func (s *exchangeServer) StreamIngest(
	stream crawlrpc.CrawlExchange_StreamIngestServer,
) error {
	ctx := stream.Context()
	requests := make(chan *crawlrpc.IngestBatchMessage, yagocrawlcontract.MaximumIngestBatchRecords)
	results := make(chan ingestStreamOutcome, yagocrawlcontract.MaximumIngestBatchRecords)
	receiveErrors := make(chan error, 1)
	go readIngestStream(ctx, stream, requests, receiveErrors)
	active := 0
	readClosed := false
	for {
		if readClosed && active == 0 && len(requests) == 0 {
			return nil
		}
		var nextRequest <-chan *crawlrpc.IngestBatchMessage
		if active < yagocrawlcontract.MaximumIngestBatchRecords {
			nextRequest = requests
		}
		select {
		case message := <-nextRequest:
			started, err := startIngestStreamRequest(ctx, stream, s, message, results)
			if err != nil {
				return err
			}
			if started {
				active++
			}
		case outcome := <-results:
			active--
			if err := sendIngestStreamOutcome(stream, outcome); err != nil {
				return err
			}
		case receiveErr := <-receiveErrors:
			receiveErrors = nil
			if errors.Is(receiveErr, io.EOF) {
				readClosed = true
				continue
			}
			return receiveErr
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
}

func startIngestStreamRequest(
	ctx context.Context,
	stream crawlrpc.CrawlExchange_StreamIngestServer,
	server *exchangeServer,
	message *crawlrpc.IngestBatchMessage,
	results chan<- ingestStreamOutcome,
) (bool, error) {
	if message == nil {
		return false, nil
	}
	if message.GetSubmissionId() == "" {
		err := sendIngestStreamAcknowledgment(stream, &crawlrpc.IngestAck{
			SubmissionId: message.GetSubmissionId(),
			ErrorCode:    int32(codes.InvalidArgument),
		})

		return false, err
	}
	go submitStreamIngest(ctx, server, message, results)

	return true, nil
}

func sendIngestStreamOutcome(
	stream crawlrpc.CrawlExchange_StreamIngestServer,
	outcome ingestStreamOutcome,
) error {
	acknowledgment := outcome.acknowledgment
	if acknowledgment == nil {
		acknowledgment = &crawlrpc.IngestAck{}
	}
	acknowledgment.SubmissionId = outcome.submissionID
	acknowledgment.ErrorCode = int32(outcome.errorCode)

	return sendIngestStreamAcknowledgment(stream, acknowledgment)
}

func sendIngestStreamAcknowledgment(
	stream crawlrpc.CrawlExchange_StreamIngestServer,
	acknowledgment *crawlrpc.IngestAck,
) error {
	if err := stream.Send(acknowledgment); err != nil {
		return fmt.Errorf("send ingest acknowledgment: %w", err)
	}

	return nil
}

func readIngestStream(
	ctx context.Context,
	stream crawlrpc.CrawlExchange_StreamIngestServer,
	requests chan<- *crawlrpc.IngestBatchMessage,
	receiveErrors chan<- error,
) {
	for {
		message, err := stream.Recv()
		if err != nil {
			receiveErrors <- err
			return
		}
		select {
		case requests <- message:
		case <-ctx.Done():
			return
		}
	}
}

func submitStreamIngest(
	ctx context.Context,
	server *exchangeServer,
	message *crawlrpc.IngestBatchMessage,
	results chan<- ingestStreamOutcome,
) {
	acknowledgment, err := server.SubmitIngest(ctx, message)
	outcome := ingestStreamOutcome{
		submissionID:   message.GetSubmissionId(),
		acknowledgment: acknowledgment,
	}
	if err != nil {
		outcome.errorCode = status.Code(err)
	}
	results <- outcome
}
