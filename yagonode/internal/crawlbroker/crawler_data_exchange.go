package crawlbroker

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type SearchDemandReader interface {
	ReadPage(context.Context, string, int) (yagocrawlcontract.SearchDemandPage, error)
}

type CrawlerDataExchangeConfig struct {
	Demand         SearchDemandReader
	AuthorizeKey   func(context.Context, string) bool
	RecordProgress func(context.Context, yagocrawlcontract.CrawlRunProgress)
}

type crawlerDataExchangeServer struct {
	crawlrpc.UnimplementedCrawlerDataExchangeServer
	config      CrawlerDataExchangeConfig
	ingest      chan<- crawlresults.IngestDelivery
	beginIngest func() func()
}

func newCrawlerDataExchangeServer(
	config CrawlerDataExchangeConfig,
	ingest chan<- crawlresults.IngestDelivery,
	beginIngest func() func(),
) *crawlerDataExchangeServer {
	return &crawlerDataExchangeServer{
		config: config, ingest: ingest, beginIngest: beginIngest,
	}
}

func (server *crawlerDataExchangeServer) ReadSearchDemand(
	ctx context.Context,
	request *crawlrpc.SearchDemandRequest,
) (*crawlrpc.SearchDemandPage, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "search demand request is required")
	}
	if err := server.authorize(ctx); err != nil {
		return nil, err
	}
	if server.config.Demand == nil {
		return nil, status.Error(codes.Unavailable, "search demand is unavailable")
	}
	page, err := server.config.Demand.ReadPage(
		ctx,
		request.GetCursor(),
		int(request.GetPageSize()),
	)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid search demand page request")
	}
	response := &crawlrpc.SearchDemandPage{NextCursor: page.NextCursor}
	for _, record := range page.Records {
		response.Records = append(response.Records, &crawlrpc.SearchDemandRecord{
			Topic: record.Topic, Url: record.URL, Shown: record.Shown, Clicked: record.Clicked,
			LastSeenUnixNano:    unixNano(record.LastSeen),
			LastClickedUnixNano: unixNano(record.LastClicked),
			FocusedAtUnixNano:   unixNano(record.FocusedAt),
		})
	}
	return response, nil
}

func (server *crawlerDataExchangeServer) StreamDocumentMutations(
	stream crawlrpc.CrawlerDataExchange_StreamDocumentMutationsServer,
) error {
	if err := server.authorize(stream.Context()); err != nil {
		return err
	}
	requests := make(chan *crawlrpc.DocumentMutation, yagocrawlcontract.MaximumIngestBatchRecords)
	results := make(chan dataMutationOutcome, yagocrawlcontract.MaximumIngestBatchRecords)
	receiveErrors := make(chan error, 1)
	go receiveDocumentMutations(stream, requests, receiveErrors)
	active := 0
	readClosed := false
	for {
		if readClosed && active == 0 && len(requests) == 0 {
			return nil
		}
		var nextRequest <-chan *crawlrpc.DocumentMutation
		if active < yagocrawlcontract.MaximumIngestBatchRecords {
			nextRequest = requests
		}
		select {
		case message := <-nextRequest:
			if message == nil {
				continue
			}
			active++
			go func() {
				acknowledgment, err := server.absorbMutation(stream.Context(), message)
				results <- dataMutationOutcome{
					acknowledgment: acknowledgment,
					mutationID:     message.GetMutationId(),
					err:            err,
				}
			}()
		case outcome := <-results:
			active--
			acknowledgment := outcome.acknowledgment
			if acknowledgment == nil {
				acknowledgment = &crawlrpc.DocumentMutationAck{MutationId: outcome.mutationID}
			}
			if outcome.err != nil {
				acknowledgment.ErrorCode = int32(status.Code(outcome.err))
			}
			if err := stream.Send(acknowledgment); err != nil {
				return err
			}
		case receiveErr := <-receiveErrors:
			receiveErrors = nil
			if errors.Is(receiveErr, io.EOF) {
				readClosed = true
				continue
			}
			return receiveErr
		case <-stream.Context().Done():
			return status.FromContextError(stream.Context().Err()).Err()
		}
	}
}

func receiveDocumentMutations(
	stream crawlrpc.CrawlerDataExchange_StreamDocumentMutationsServer,
	requests chan<- *crawlrpc.DocumentMutation,
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
		case <-stream.Context().Done():
			return
		}
	}
}

func unixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}

type dataMutationOutcome struct {
	acknowledgment *crawlrpc.DocumentMutationAck
	mutationID     string
	err            error
}

func (server *crawlerDataExchangeServer) authorize(ctx context.Context) error {
	if server.config.AuthorizeKey == nil {
		return status.Error(codes.Unauthenticated, "crawler API key required")
	}
	values, _ := metadata.FromIncomingContext(ctx)
	for _, value := range values.Get("authorization") {
		key, ok := strings.CutPrefix(value, "Bearer ")
		if ok && key != "" && server.config.AuthorizeKey(ctx, key) {
			return nil
		}
	}
	return status.Error(codes.Unauthenticated, "crawler API key rejected")
}

func (server *crawlerDataExchangeServer) absorbMutation(
	ctx context.Context,
	message *crawlrpc.DocumentMutation,
) (*crawlrpc.DocumentMutationAck, error) {
	acknowledgment := &crawlrpc.DocumentMutationAck{MutationId: message.GetMutationId()}
	if acknowledgment.MutationId == "" {
		return nil, status.Error(codes.InvalidArgument, "document mutation identity is required")
	}
	encoded := message.GetBatchJson()
	if len(encoded) > yagocrawlcontract.MaximumIngestBatchBytes {
		return nil, status.Error(codes.InvalidArgument, "document mutation exceeds size limit")
	}
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(encoded)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "document mutation is invalid")
	}
	if batch.ObservationID == "" {
		batch.ObservationID = acknowledgment.MutationId
	}
	if batch.ObservedAt.IsZero() {
		batch.ObservedAt = batch.Document.FetchedAt
		if batch.ObservedAt.IsZero() {
			batch.ObservedAt = time.Now().UTC()
		}
	}
	finish := func() {}
	if server.beginIngest != nil {
		finish = server.beginIngest()
	}
	result := make(chan ingestOutcome, 1)
	delivery := crawlresults.IngestDelivery{
		Batch: batch, BatchJSONSize: len(encoded), SubmittedAt: time.Now(),
		Ack: func(context.Context) error {
			finish()
			result <- ingestOutcome{}
			return nil
		},
		Reject: func(_ context.Context, rule string) error {
			finish()
			result <- ingestOutcome{rejectionRule: rule}
			return nil
		},
		Nak: func(context.Context) error {
			finish()
			result <- ingestOutcome{err: errIngestDeferred}
			return nil
		},
	}
	select {
	case server.ingest <- delivery:
	case <-ctx.Done():
		finish()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	select {
	case outcome := <-result:
		if outcome.err != nil {
			return nil, status.Error(codes.Unavailable, outcome.err.Error())
		}
		acknowledgment.Rejected = outcome.rejectionRule != ""
		acknowledgment.RejectionRule = outcome.rejectionRule
		return acknowledgment, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}
