package crawlorder

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type grpcOrderReceiverConfig struct {
	activeFetches         func() uint32
	fetchAdmissionWaiters func() uint32
	terminalSettlements   crawlsettlement.Outbox
	workerSessionID       string
	leaseGrants           *crawllease.GrantRegistry
	storageSnapshot       func() yagocrawlcontract.StoragePressureSnapshot
	storagePolicy         func(yagocrawlcontract.StoragePressurePolicy)
	urlDenylist           *crawldenylist.Denylist
	runtimePolicy         func(yagocrawlcontract.CrawlerRuntimePolicy)
	runtimePolicySource   func() yagocrawlcontract.CrawlerRuntimePolicy
	fetchStartSession     FetchStartSession
}

func WithURLDenylist(denylist *crawldenylist.Denylist) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.urlDenylist = denylist
	}
}

type GRPCOrderReceiverOption func(*grpcOrderReceiverConfig)

type workerSessionHeartbeatReport struct {
	workerID                 string
	workerSessionID          string
	activeFetches            func() uint32
	fetchAdmissionWaiters    func() uint32
	activeLeaseIDs           []string
	acknowledgedDirectiveIDs []uint64
}

func WithHeartbeatActiveFetches(activeFetches func() uint32) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.activeFetches = activeFetches
	}
}

func WithHeartbeatFetchAdmissionWaiters(waiters func() uint32) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.fetchAdmissionWaiters = waiters
	}
}

func WithHeartbeatStoragePressure(
	snapshot func() yagocrawlcontract.StoragePressureSnapshot,
	apply func(yagocrawlcontract.StoragePressurePolicy),
) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.storageSnapshot = snapshot
		config.storagePolicy = apply
	}
}

func WithHeartbeatRuntimePolicy(
	effective func() yagocrawlcontract.CrawlerRuntimePolicy,
	apply func(yagocrawlcontract.CrawlerRuntimePolicy),
) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.runtimePolicySource = effective
		config.runtimePolicy = apply
	}
}

func WithWorkerLeaseSession(
	workerSessionID string,
	leaseGrants *crawllease.GrantRegistry,
) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.workerSessionID = workerSessionID
		config.leaseGrants = leaseGrants
	}
}

func workerHeartbeat(
	workerID string,
	activeFetches func() uint32,
	acknowledgedDirectiveIDs ...uint64,
) *crawlrpc.WorkerHeartbeat {
	return workerHeartbeatWithWaiters(workerID, activeFetches, nil, acknowledgedDirectiveIDs...)
}

func workerHeartbeatWithWaiters(
	workerID string,
	activeFetches func() uint32,
	fetchAdmissionWaiters func() uint32,
	acknowledgedDirectiveIDs ...uint64,
) *crawlrpc.WorkerHeartbeat {
	heartbeat := &crawlrpc.WorkerHeartbeat{
		WorkerId:                 workerID,
		AcknowledgedDirectiveIds: append([]uint64(nil), acknowledgedDirectiveIDs...),
	}
	if activeFetches != nil {
		active := activeFetches()
		heartbeat.ActiveFetches = &active
	}
	if fetchAdmissionWaiters != nil {
		waiting := fetchAdmissionWaiters()
		heartbeat.FetchAdmissionWaiting = &waiting
	}

	return heartbeat
}

func workerSessionHeartbeat(report workerSessionHeartbeatReport) *crawlrpc.WorkerHeartbeat {
	heartbeat := workerHeartbeatWithWaiters(
		report.workerID,
		report.activeFetches,
		report.fetchAdmissionWaiters,
		report.acknowledgedDirectiveIDs...,
	)
	heartbeat.WorkerSessionId = report.workerSessionID
	heartbeat.ActiveLeaseIds = append([]string(nil), report.activeLeaseIDs...)

	return heartbeat
}
