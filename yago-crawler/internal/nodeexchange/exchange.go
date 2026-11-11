package nodeexchange

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	callTimeout          = time.Second
	terminalCallTimeout  = 30 * time.Second
	messageTerminalRetry = "crawler terminal acknowledgment will retry"
)

type Exchange struct {
	connection *grpc.ClientConn
	client     crawlrpc.CrawlExchangeClient
	workerID   string
	sessionID  string
	apiKey     string
	dataClient crawlrpc.CrawlerDataExchangeClient
	heartbeat  sync.Mutex
}

func Connect(target, workerID, sessionID, apiKey string) (*Exchange, error) {
	if target == "" || strings.TrimSpace(apiKey) == "" ||
		!yagocrawlcontract.ValidCrawlerWorkerIdentity(workerID) ||
		!yagocrawlcontract.ValidCrawlerSessionIdentity(sessionID) {
		return nil, errors.New("invalid crawler node exchange identity")
	}
	connection, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create crawler node connection: %w", err)
	}
	return &Exchange{
		connection: connection,
		client:     crawlrpc.NewCrawlExchangeClient(connection),
		dataClient: crawlrpc.NewCrawlerDataExchangeClient(connection),
		workerID:   workerID,
		sessionID:  sessionID,
		apiKey:     strings.TrimSpace(apiKey),
	}, nil
}

func (exchange *Exchange) Close() error {
	if err := exchange.connection.Close(); err != nil {
		return fmt.Errorf("close crawler node connection: %w", err)
	}
	return nil
}

func (exchange *Exchange) ReadRuntimePolicy(ctx context.Context) (yagocrawlcontract.CrawlerRuntimePolicy, error) {
	requestContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	requestContext = metadata.AppendToOutgoingContext(
		requestContext,
		"authorization",
		"Bearer "+exchange.apiKey,
	)
	message, err := exchange.client.ReadRuntimePolicy(
		requestContext,
		&crawlrpc.CrawlerRuntimePolicyRequest{WorkerId: exchange.workerID},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return yagocrawlcontract.CrawlerRuntimePolicy{}, fmt.Errorf("read crawler runtime policy: %w", err)
	}
	policy, err := yagocrawlcontract.CrawlerRuntimePolicyFromProtoWithFallback(
		message,
		yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	)
	if err != nil {
		return yagocrawlcontract.CrawlerRuntimePolicy{}, fmt.Errorf("decode crawler runtime policy: %w", err)
	}
	return policy, nil
}

func (exchange *Exchange) Heartbeat(
	ctx context.Context,
	leaseIDs []string,
	activeFetches uint32,
	activePages []*crawlrpc.ActiveCrawlPage,
	waitingPages []*crawlrpc.WaitingCrawlPage,
	urlDenylistRevision []byte,
	confirmDeliveries bool,
	acknowledgedDirectiveIDs []uint64,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	exchange.heartbeat.Lock()
	defer exchange.heartbeat.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	requestContext = metadata.AppendToOutgoingContext(
		requestContext,
		"authorization",
		"Bearer "+exchange.apiKey,
	)
	active := activeFetches
	confirm := confirmDeliveries
	result, err := exchange.client.Heartbeat(requestContext, &crawlrpc.WorkerHeartbeat{
		WorkerId:                     exchange.workerID,
		WorkerSessionId:              exchange.sessionID,
		ActiveFetches:                &active,
		ActivePages:                  activePages,
		WaitingPages:                 waitingPages,
		UrlDenylistRevision:          append([]byte(nil), urlDenylistRevision...),
		ActiveLeaseIds:               append([]string(nil), leaseIDs...),
		ConfirmActiveLeaseDeliveries: &confirm,
		AcknowledgedDirectiveIds:     append([]uint64(nil), acknowledgedDirectiveIDs...),
	})
	if err != nil {
		return nil, fmt.Errorf("send crawler heartbeat: %w", err)
	}
	return result, nil
}

func (exchange *Exchange) StreamOrders(
	ctx context.Context,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	return exchange.client.StreamOrders(ctx, &crawlrpc.WorkerRegistration{
		WorkerId:         exchange.workerID,
		WorkerSessionId:  exchange.sessionID,
		FetchStartLeases: true,
	})
}

func (exchange *Exchange) LeaseFetchStarts(
	ctx context.Context,
	request *crawlrpc.FetchStartLeaseRequest,
) (*crawlrpc.FetchStartLeaseDecision, error) {
	requestContext, cancel := context.WithTimeout(
		ctx,
		yagocrawlcontract.MaximumFetchStartPermitDeliveryAllowance,
	)
	defer cancel()
	return exchange.client.LeaseFetchStarts(requestContext, request)
}

func (exchange *Exchange) ReportProgress(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	leaseID string,
	tally yagocrawlcontract.CrawlRunTally,
) error {
	requestContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	requestContext = metadata.AppendToOutgoingContext(
		requestContext,
		"authorization",
		"Bearer "+exchange.apiKey,
	)
	_, err := exchange.client.ReportProgress(requestContext, &crawlrpc.CrawlProgressReport{
		WorkerId:        exchange.workerID,
		RunId:           append([]byte(nil), order.Provenance...),
		ProfileHandle:   order.Profile.Handle,
		ProfileName:     order.Profile.Name,
		State:           crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
		Tally:           tallyToProto(tally),
		LeaseId:         leaseID,
		WorkerSessionId: exchange.sessionID,
	})
	if err != nil {
		return fmt.Errorf("report crawl progress: %w", err)
	}
	return nil
}

func (exchange *Exchange) AcknowledgeTerminal(
	ctx context.Context,
	leaseID string,
	orderJSON []byte,
	tally yagocrawlcontract.CrawlRunTally,
	state crawlrpc.CrawlRunState,
) error {
	identity := sha256.Sum256(orderJSON)
	request := &crawlrpc.OrderAck{
		LeaseId:         leaseID,
		OrderIdentity:   identity[:],
		WorkerId:        exchange.workerID,
		WorkerSessionId: exchange.sessionID,
		TerminalState:   state,
		TerminalTally:   tallyToProto(tally),
	}
	settlementContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalCallTimeout)
	defer cancel()
	var lastErr error
	for settlementContext.Err() == nil {
		result, err := exchange.client.AckOrder(settlementContext, request)
		if err == nil && len(result.GetConfirmationToken()) == 0 {
			err = errors.New("node returned an empty terminal confirmation token")
		}
		if err == nil {
			confirmation := *request
			confirmation.ConfirmationToken = append([]byte(nil), result.GetConfirmationToken()...)
			_, err = exchange.client.AckOrder(settlementContext, &confirmation)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		slog.WarnContext(settlementContext, messageTerminalRetry, slog.Any("error", err))
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-timer.C:
		case <-settlementContext.Done():
			timer.Stop()
		}
	}
	return fmt.Errorf("terminal crawl acknowledgment timed out: %w", errors.Join(lastErr, settlementContext.Err()))
}

func (exchange *Exchange) Requeue(ctx context.Context, leaseID string) error {
	requestContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalCallTimeout)
	defer cancel()
	_, err := exchange.client.AckOrder(requestContext, &crawlrpc.OrderAck{
		LeaseId:         leaseID,
		Requeue:         true,
		WorkerId:        exchange.workerID,
		WorkerSessionId: exchange.sessionID,
	})
	if err != nil {
		return fmt.Errorf("requeue crawl order: %w", err)
	}
	return nil
}

func tallyToProto(tally yagocrawlcontract.CrawlRunTally) *crawlrpc.CrawlRunTally {
	converted := &crawlrpc.CrawlRunTally{
		Fetched:            tally.Fetched,
		Indexed:            tally.Indexed,
		Failed:             tally.Failed,
		RobotsDenied:       tally.RobotsDenied,
		Duplicates:         tally.Duplicates,
		Pending:            tally.Pending,
		DocumentCandidates: tally.DocumentCandidates,
		RemovalMutations:   tally.RemovalMutations,
		NotIndexable:       tally.NotIndexable,
		NodeRejected:       tally.NodeRejected,
		PendingIngest:      tally.PendingIngest,
		OutcomeCounts:      make([]*crawlrpc.CrawlOutcomeCount, 0, len(tally.OutcomeCounts)),
	}
	for _, count := range tally.OutcomeCounts {
		converted.OutcomeCounts = append(converted.OutcomeCounts, &crawlrpc.CrawlOutcomeCount{
			Class: count.Class, Reason: count.Reason, Pages: count.Pages,
		})
	}
	return converted
}
