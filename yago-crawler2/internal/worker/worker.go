package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler2/internal/nodeexchange"
	"github.com/D4rk4/yago/yago-crawler2/internal/robots"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagoegress"
)

const (
	heartbeatInterval    = yagocrawlcontract.DefaultWorkerHeartbeatInterval
	messageOrderStream   = "crawler order stream reconnecting"
	messageHeartbeat     = "crawler heartbeat failed"
	messageOrderRejected = "crawler order rejected"
	messageOrderFailed   = "crawler order processing failed"
	messageOrderDone     = "crawler order finished"
	messageBadDirective  = "crawler control directive is not supported"
	maximumBodyBytes     = int64(4 << 20)
)

type Configuration struct {
	NodeRPCAddress string
	DataDirectory  string
	WorkerID       string
}

type Worker struct {
	exchange                        *nodeexchange.Exchange
	frontier                        *frontier.Frontier
	fetcher                         *httpfetch.PageFetcher
	orders                          *orderAdmission
	fetchAdmission                  *fetchAdmission
	hosts                           *hostAdmission
	robots                          *robots.Policy
	policy                          yagocrawlcontract.CrawlerRuntimePolicy
	workerID                        string
	sessionID                       string
	mutex                           sync.RWMutex
	progressMutex                   sync.Mutex
	progressWake                    chan struct{}
	pendingProgress                 map[string]progressUpdate
	pendingIngestMutex              sync.Mutex
	pendingIngestActive             map[string]struct{}
	heartbeatMutex                  sync.Mutex
	pendingDirectiveAcknowledgments []uint64
	leases                          map[string]struct{}
	activePages                     map[string]pageActivity
	active                          atomic.Uint32
}

func Open(ctx context.Context, configuration Configuration) (*Worker, error) {
	sessionID, err := newSessionIdentity()
	if err != nil {
		return nil, fmt.Errorf("create crawler session identity: %w", err)
	}
	exchange, err := nodeexchange.Connect(
		configuration.NodeRPCAddress,
		configuration.WorkerID,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	policy, err := exchange.ReadRuntimePolicy(ctx)
	if err != nil {
		_ = exchange.Close()
		return nil, err
	}
	if policy.FrontierStateMaximumBytes == 0 {
		policy.FrontierStateMaximumBytes = yagocrawlcontract.DefaultCrawlerFrontierStateMaximumBytes
	}
	logRuntimePolicy(ctx, policy)
	frontierPath := filepath.Join(configuration.DataDirectory, "yago-crawler2", "frontier.db")
	workFrontier, err := frontier.Open(frontierPath, frontier.Config{
		MaximumStateBytes: policy.FrontierStateMaximumBytes,
		LeaseDuration:     max(policy.RequestTimeout*2, time.Minute),
		HostDelay:         policy.CrawlDelay,
		MaximumHostActive: policy.MaximumHostConcurrency,
	})
	if err != nil {
		_ = exchange.Close()
		return nil, err
	}
	fetcher, err := httpfetch.NewPageFetcher(httpfetch.Limits{
		RequestTimeout:      policy.RequestTimeout,
		ConnectTimeout:      policy.ConnectTimeout,
		TLSHandshakeTimeout: policy.TLSTimeout,
		HeaderTimeout:       policy.HeaderTimeout,
		MaximumRedirects:    yagocrawlcontract.DefaultMaximumPageRedirects,
		MaximumResponseSize: maximumBodyBytes,
		MaximumIdleHosts:    128,
		MaximumHostActive:   policy.MaximumHostConcurrency,
		UserAgent:           policy.UserAgent,
	}, yagoegress.NewGuard(
		policy.AllowPrivateNetworks,
		yagoegress.WithPrivateAllowlist(policy.AllowedPrivateCIDRs),
	))
	if err != nil {
		_ = workFrontier.Close()
		_ = exchange.Close()
		return nil, err
	}
	runner := &Worker{
		exchange:            exchange,
		frontier:            workFrontier,
		fetcher:             fetcher,
		orders:              newOrderAdmission(),
		fetchAdmission:      newFetchAdmission(yagocrawlcontract.DefaultFetchWorkerConcurrency),
		hosts:               newHostAdmission(policy.CrawlDelay, policy.MaximumHostConcurrency),
		policy:              policy,
		workerID:            configuration.WorkerID,
		sessionID:           sessionID,
		leases:              make(map[string]struct{}),
		progressWake:        make(chan struct{}, 1),
		pendingProgress:     make(map[string]progressUpdate),
		pendingIngestActive: make(map[string]struct{}),
		activePages:         make(map[string]pageActivity),
	}
	runner.robots = robots.NewPolicy(runner.fetchPage, policy.UserAgent, policy.CrawlDelay)
	return runner, nil
}

func (worker *Worker) Close() error {
	worker.fetcher.CloseIdleConnections()
	return errors.Join(worker.frontier.Close(), worker.exchange.Close())
}

func (worker *Worker) Run(ctx context.Context) error {
	go worker.runPendingIngest(ctx)
	go worker.runProgressReports(ctx)
	for ctx.Err() == nil {
		streamContext, stopStream := context.WithCancel(ctx)
		stream, err := worker.exchange.StreamOrders(streamContext)
		if err != nil {
			stopStream()
			if ctx.Err() != nil {
				return nil
			}
			slog.WarnContext(ctx, messageOrderStream, slog.Any("error", err))
		} else {
			heartbeatContext, stopHeartbeats := context.WithCancel(ctx)
			go worker.runHeartbeats(heartbeatContext)
			var replay recoveredReplay
			var activeOrders sync.WaitGroup
			for {
				message, receiveErr := stream.Recv()
				if receiveErr != nil {
					stopStream()
					if ctx.Err() == nil {
						slog.WarnContext(ctx, messageOrderStream, slog.Any("error", receiveErr))
					}
					break
				}
				worker.trackLease(message.GetLeaseId())
				if err := worker.orders.Acquire(streamContext); err != nil {
					_ = worker.exchange.Requeue(ctx, message.GetLeaseId())
					worker.releaseLease(message.GetLeaseId())
					if ctx.Err() == nil {
						slog.WarnContext(ctx, messageOrderFailed, slog.Any("error", err))
					}
					stopStream()
					break
				}
				if err := worker.confirmOrderDelivery(streamContext, message, &replay); err != nil {
					worker.orders.Release()
					slog.WarnContext(ctx, messageOrderRejected, slog.Any("error", err))
					stopStream()
					break
				}
				activeOrders.Add(1)
				go func(message *crawlrpc.CrawlOrderMessage) {
					defer activeOrders.Done()
					defer worker.orders.Release()
					if err := worker.receiveOrder(streamContext, message); err != nil {
						slog.WarnContext(ctx, messageOrderFailed, slog.Any("error", err))
						stopStream()
					}
				}(message)
			}
			stopStream()
			stopHeartbeats()
			activeOrders.Wait()
			worker.clearLeases()
		}
		if ctx.Err() != nil {
			return nil
		}
		if !waitContext(ctx, time.Second) {
			return nil
		}
	}
	return nil
}

func (worker *Worker) runHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := worker.sendHeartbeat(
				ctx,
				worker.activeLeaseIDs(),
				worker.active.Load(),
				true,
			); err != nil {
				slog.WarnContext(ctx, messageHeartbeat, slog.Any("error", err))
			}
		}
	}
}

func (worker *Worker) trackLease(leaseID string) {
	worker.mutex.Lock()
	worker.leases[leaseID] = struct{}{}
	worker.mutex.Unlock()
}

func (worker *Worker) releaseLease(leaseID string) {
	worker.mutex.Lock()
	delete(worker.leases, leaseID)
	worker.mutex.Unlock()
}

func (worker *Worker) clearLeases() {
	worker.mutex.Lock()
	clear(worker.leases)
	worker.mutex.Unlock()
}

func (worker *Worker) activeLeaseIDs() []string {
	worker.mutex.RLock()
	leaseIDs := make([]string, 0, len(worker.leases))
	for leaseID := range worker.leases {
		leaseIDs = append(leaseIDs, leaseID)
	}
	worker.mutex.RUnlock()
	sort.Strings(leaseIDs)
	return leaseIDs
}

func (worker *Worker) hasLease(leaseID string) bool {
	worker.mutex.RLock()
	_, found := worker.leases[leaseID]
	worker.mutex.RUnlock()
	return found
}

func newSessionIdentity() (string, error) {
	var identity [24]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(identity[:]), nil
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
