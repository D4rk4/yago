package crawlbroker

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type CrawlerDataExchangeRuntime struct {
	Ingest   *IngestReceiver
	Control  *ControlRegistry
	policy   *runtimePolicyCrawlExchange
	server   *grpc.Server
	listener net.Listener
}

func OpenCrawlerDataExchange(config Config, storage *vault.Vault) (*CrawlerDataExchangeRuntime, error) {
	defaults, err := crawlerControlDefaultsFor(config)
	if err != nil {
		return nil, err
	}
	control, err := newPersistentControlRegistry(storage, defaults)
	if err != nil {
		return nil, fmt.Errorf("open crawler policy registry: %w", err)
	}
	listener, err := listenCrawlRPC(config.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen crawler data exchange %q: %w", config.ListenAddr, err)
	}
	ingest := newIngestReceiver()
	server := newGRPCServer()
	policy := &runtimePolicyCrawlExchange{
		control:     control,
		data:        newCrawlerDataExchangeServer(config.DataExchange, ingest.out, ingest.beginIngest),
		progress:    config.DataExchange.RecordProgress,
		urlDenylist: newCrawlURLDenylistDelivery(),
	}
	crawlrpc.RegisterCrawlExchangeServer(server, policy)
	crawlrpc.RegisterCrawlerDataExchangeServer(
		server,
		policy.data,
	)
	go func() { _ = server.Serve(listener) }()
	return &CrawlerDataExchangeRuntime{
		Ingest: ingest, Control: control, policy: policy, server: server, listener: listener,
	}, nil
}

func (runtime *CrawlerDataExchangeRuntime) SetURLDenylistSource(source CrawlURLDenylistSource) {
	if runtime != nil && runtime.policy != nil {
		runtime.policy.urlDenylist.SetSource(source)
	}
}

func (runtime *CrawlerDataExchangeRuntime) Close() {
	if runtime == nil {
		return
	}
	if runtime.server != nil {
		runtime.server.Stop()
	}
	if runtime.listener != nil {
		_ = runtime.listener.Close()
	}
}

type runtimePolicyCrawlExchange struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	control     *ControlRegistry
	data        *crawlerDataExchangeServer
	progress    func(context.Context, yagocrawlcontract.CrawlRunProgress)
	urlDenylist *crawlURLDenylistDelivery
}

func (server *runtimePolicyCrawlExchange) ReadRuntimePolicy(
	ctx context.Context,
	request *crawlrpc.CrawlerRuntimePolicyRequest,
) (*crawlrpc.CrawlerRuntimePolicy, error) {
	if err := server.data.authorize(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "crawler policy request is required")
	}
	if !yagocrawlcontract.ValidCrawlerWorkerIdentity(request.GetWorkerId()) {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler worker identity")
	}
	policy, err := crawlerStartupRuntimePolicy(
		server.control.RuntimePolicy(),
		server.control.StoragePressurePolicy(),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid crawler runtime policy")
	}
	return policy, nil
}

func (server *runtimePolicyCrawlExchange) Heartbeat(
	ctx context.Context,
	request *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "crawler heartbeat is required")
	}
	if err := server.data.authorize(ctx); err != nil {
		return nil, err
	}
	workerID := request.GetWorkerId()
	sessionID := request.GetWorkerSessionId()
	if !validCrawlerLeaseIdentity(workerID, sessionID) || !validHeartbeatFetchActivity(request) {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler heartbeat identity or activity")
	}
	if len(request.GetActiveLeaseIds()) != 0 {
		return nil, status.Error(codes.InvalidArgument, "crawler order leases are not supported")
	}
	activePages, valid := activeCrawlerPagesFromHeartbeat(request.GetActivePages())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid active crawler pages")
	}
	waitingPages, valid := waitingCrawlerPagesFromHeartbeat(request.GetWaitingPages())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid waiting crawler pages")
	}
	denylist, err := server.urlDenylistForHeartbeat(request)
	if err != nil {
		return nil, err
	}
	if request.GetUrlDenylistBootstrap() {
		if !validURLDenylistBootstrapHeartbeat(request) {
			return nil, status.Error(codes.InvalidArgument, "invalid crawler denylist bootstrap heartbeat")
		}
		return &crawlrpc.WorkerHeartbeatResult{UrlDenylist: denylist}, nil
	}
	acknowledged, valid := normalizedHeartbeatDirectiveAcknowledgments(
		request.GetAcknowledgedDirectiveIds(),
	)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler directive acknowledgments")
	}
	server.control.registerDataExchangeWorker(workerID)
	server.control.recordHeartbeatFetchActivity(
		workerID,
		sessionID,
		request.ActiveFetches,
		request.FetchAdmissionWaiting,
	)
	server.control.recordHeartbeatActivePages(workerID, sessionID, activePages)
	server.control.recordHeartbeatWaitingPages(workerID, sessionID, waitingPages)
	server.control.recordStoragePressure(workerID, request)
	directives, err := server.control.deliverForHeartbeat(ctx, workerID, acknowledged)
	if err != nil {
		return nil, status.Error(codes.Internal, "read crawler control directives")
	}
	policy, err := crawlerStartupRuntimePolicy(
		server.control.RuntimePolicy(),
		server.control.StoragePressurePolicy(),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid crawler runtime policy")
	}
	return &crawlrpc.WorkerHeartbeatResult{
		Directives:    directivesToProto(directives),
		UrlDenylist:   denylist,
		RuntimePolicy: policy,
	}, nil
}

func (server *runtimePolicyCrawlExchange) ReportProgress(
	ctx context.Context,
	request *crawlrpc.CrawlProgressReport,
) (*crawlrpc.CrawlProgressAck, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "crawler progress report is required")
	}
	if err := server.data.authorize(ctx); err != nil {
		return nil, err
	}
	if !validCrawlerLeaseIdentity(request.GetWorkerId(), request.GetWorkerSessionId()) ||
		len(request.GetRunId()) != yagomodel.HashLength || request.GetLeaseId() != "" ||
		request.GetTally() == nil || server.progress == nil ||
		request.GetProfileName() != "search-demand" {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler frontier progress report")
	}
	progress := progressFromReport(request)
	if !progress.Tally.Valid() {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler outcome tally")
	}
	progress.State = yagocrawlcontract.CrawlRunRunning
	server.control.registerDataExchangeWorker(request.GetWorkerId())
	server.progress(ctx, progress)
	return &crawlrpc.CrawlProgressAck{}, nil
}

func (server *runtimePolicyCrawlExchange) urlDenylistForHeartbeat(
	request *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.CrawlURLDenylist, error) {
	revision := request.GetUrlDenylistRevision()
	if !yagocrawlcontract.ValidCrawlURLDenylistRevision(revision) {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler URL denylist revision")
	}
	policy, err := server.urlDenylist.Snapshot(revision)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "crawler URL denylist is unavailable")
	}
	return policy, nil
}
