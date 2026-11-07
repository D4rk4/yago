package crawlorder

import (
	"context"
	"log/slog"
	"sync"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	msgOrderDecodeFailed        = "crawl order decode failed"
	msgOrderStreamReconnect     = "crawl order stream reconnecting"
	msgOrderStreamReceiveFailed = "crawl order stream receive failed"
	msgHeartbeatFailed          = "crawl worker heartbeat failed"

	DefaultOrderRetryWait          = time.Second
	DefaultHeartbeatInterval       = yagocrawlcontract.DefaultWorkerHeartbeatInterval
	DefaultAckTimeout              = 5 * time.Second
	DefaultStartupHeartbeatTimeout = time.Second
)

var (
	orderStreamRetryWait         = DefaultOrderRetryWait
	orderHeartbeatInterval       = DefaultHeartbeatInterval
	orderAckTimeout              = DefaultAckTimeout
	orderStartupHeartbeatTimeout = DefaultStartupHeartbeatTimeout
)

// OrderStreamer is the slice of the node's CrawlExchange client the receiver
// needs: a server-streaming subscription for leased crawl orders plus the calls
// that settle a lease and keep it alive.
type OrderStreamer interface {
	StreamOrders(
		ctx context.Context,
		in *crawlrpc.WorkerRegistration,
		opts ...grpc.CallOption,
	) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error)
	AckOrder(
		ctx context.Context,
		in *crawlrpc.OrderAck,
		opts ...grpc.CallOption,
	) (*crawlrpc.OrderAckResult, error)
	Heartbeat(
		ctx context.Context,
		in *crawlrpc.WorkerHeartbeat,
		opts ...grpc.CallOption,
	) (*crawlrpc.WorkerHeartbeatResult, error)
}

type GRPCOrderReceiver struct {
	out chan CrawlOrderDelivery
}

type crawlOrderStreamSession struct {
	client              OrderStreamer
	workerID            string
	out                 chan<- CrawlOrderDelivery
	retryWait           time.Duration
	terminalSettlements *terminalSettlementRelay
	heartbeat           *heartbeatDelivery
	fetchStartSession   FetchStartSession
}

type crawlOrderStreamDrain struct {
	client              OrderStreamer
	stream              grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage]
	out                 chan<- CrawlOrderDelivery
	workerID            string
	terminalSettlements *terminalSettlementRelay
	heartbeat           *heartbeatDelivery
}

type crawlOrderDeliveryEnvelope struct {
	client              OrderStreamer
	out                 chan<- CrawlOrderDelivery
	order               yagocrawlcontract.CrawlOrder
	orderIdentity       []byte
	leaseID             string
	workerID            string
	terminalSettlements *terminalSettlementRelay
	heartbeat           *heartbeatDelivery
}

func NewGRPCOrderReceiver(
	ctx context.Context,
	client OrderStreamer,
	workerID string,
	control ControlHandler,
	options ...GRPCOrderReceiverOption,
) *GRPCOrderReceiver {
	config := grpcOrderReceiverConfig{}
	for _, apply := range options {
		apply(&config)
	}
	heartbeat := heartbeatDelivery{
		requestTimeout:        orderHeartbeatRequestTimeout,
		client:                client,
		workerID:              workerID,
		workerSessionID:       config.workerSessionID,
		control:               control,
		activeFetches:         config.activeFetches,
		fetchAdmissionWaiters: config.fetchAdmissionWaiters,
		acknowledgments:       &controlAcknowledgments{},
		leaseGrants:           config.leaseGrants,
		operation:             &sync.Mutex{},
		storageSnapshot:       config.storageSnapshot,
		storagePolicy:         config.storagePolicy,
		urlDenylist:           config.urlDenylist,
		runtimePolicy:         config.runtimePolicy,
		runtimePolicySource:   config.runtimePolicySource,
	}
	out := make(chan CrawlOrderDelivery)
	if config.leaseGrants == nil || config.urlDenylist != nil {
		startupCtx, cancelStartup := context.WithTimeout(ctx, orderStartupHeartbeatTimeout)
		heartbeat.deliver(startupCtx)
		cancelStartup()
	}
	if ctx.Err() != nil {
		close(out)

		return &GRPCOrderReceiver{out: out}
	}
	var terminalSettlements *terminalSettlementRelay
	if config.terminalSettlements != nil {
		terminalSettlements = newTerminalSettlementRelay(
			client,
			config.terminalSettlements,
		)
		terminalSettlements.bindWorkerLeaseSession(
			workerID,
			config.workerSessionID,
			config.leaseGrants,
		)
		go terminalSettlements.run(ctx)
	}
	go streamCrawlOrdersWithLeaseSession(ctx, crawlOrderStreamSession{
		client:              client,
		workerID:            workerID,
		out:                 out,
		retryWait:           orderStreamRetryWait,
		terminalSettlements: terminalSettlements,
		heartbeat:           &heartbeat,
		fetchStartSession:   config.fetchStartSession,
	})
	go periodicHeartbeats(ctx, heartbeat, orderHeartbeatInterval)

	return &GRPCOrderReceiver{out: out}
}

func (r *GRPCOrderReceiver) Receive() <-chan CrawlOrderDelivery {
	return r.out
}

func streamCrawlOrdersWithLeaseSession(
	ctx context.Context,
	session crawlOrderStreamSession,
) {
	defer close(session.out)
	if session.heartbeat != nil && session.heartbeat.urlDenylist != nil &&
		!session.heartbeat.urlDenylist.Ready() &&
		!session.heartbeat.urlDenylist.Wait(ctx) {
		return
	}
	for {
		workerSessionID := ""
		if session.heartbeat != nil {
			workerSessionID = session.heartbeat.workerSessionID
		}
		streamCtx, finishStreamAttempt := orderStreamAttemptContext(ctx, session.heartbeat)
		stream, err := session.client.StreamOrders(streamCtx, &crawlrpc.WorkerRegistration{
			WorkerId:         session.workerID,
			WorkerSessionId:  workerSessionID,
			FetchStartLeases: session.fetchStartSession != nil,
		})
		if err != nil {
			if session.fetchStartSession != nil {
				session.fetchStartSession.Disconnected()
			}
			slog.WarnContext(ctx, msgOrderStreamReconnect, slog.Any("error", err))
		} else {
			if session.fetchStartSession != nil {
				session.fetchStartSession.Connected()
			}
			drainCrawlOrderMessages(streamCtx, crawlOrderStreamDrain{
				client:              session.client,
				stream:              stream,
				out:                 session.out,
				workerID:            session.workerID,
				terminalSettlements: session.terminalSettlements,
				heartbeat:           session.heartbeat,
			})
			if session.fetchStartSession != nil {
				session.fetchStartSession.Disconnected()
			}
		}
		finishStreamAttempt()
		select {
		case <-time.After(session.retryWait):
		case <-ctx.Done():
			return
		}
	}
}

func periodicHeartbeats(
	ctx context.Context,
	heartbeat heartbeatDelivery,
	interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeat.deliver(ctx)
		}
	}
}

func deliverOrderWithLeaseSession(
	ctx context.Context,
	envelope crawlOrderDeliveryEnvelope,
) bool {
	delivery := leaseSessionCrawlOrderDelivery(ctx, envelope)
	select {
	case envelope.out <- delivery:
		return true
	case <-ctx.Done():
		return false
	}
}
