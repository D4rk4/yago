package crawlorder

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type grpcOrderReceiverConfig struct {
	activeFetches       func() uint32
	terminalSettlements crawlsettlement.Outbox
	workerSessionID     string
	leaseGrants         *crawllease.GrantRegistry
	storageSnapshot     func() yagocrawlcontract.StoragePressureSnapshot
	storagePolicy       func(yagocrawlcontract.StoragePressurePolicy)
}

type GRPCOrderReceiverOption func(*grpcOrderReceiverConfig)

func WithHeartbeatActiveFetches(activeFetches func() uint32) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.activeFetches = activeFetches
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
	heartbeat := &crawlrpc.WorkerHeartbeat{
		WorkerId:                 workerID,
		AcknowledgedDirectiveIds: append([]uint64(nil), acknowledgedDirectiveIDs...),
	}
	if activeFetches != nil {
		active := activeFetches()
		heartbeat.ActiveFetches = &active
	}

	return heartbeat
}

func workerSessionHeartbeat(
	workerID string,
	workerSessionID string,
	activeFetches func() uint32,
	activeLeaseIDs []string,
	acknowledgedDirectiveIDs ...uint64,
) *crawlrpc.WorkerHeartbeat {
	heartbeat := workerHeartbeat(workerID, activeFetches, acknowledgedDirectiveIDs...)
	heartbeat.WorkerSessionId = workerSessionID
	heartbeat.ActiveLeaseIds = append([]string(nil), activeLeaseIDs...)

	return heartbeat
}
