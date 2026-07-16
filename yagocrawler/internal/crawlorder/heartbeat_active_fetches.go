package crawlorder

import "github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"

type grpcOrderReceiverConfig struct {
	activeFetches func() uint32
}

type GRPCOrderReceiverOption func(*grpcOrderReceiverConfig)

func WithHeartbeatActiveFetches(activeFetches func() uint32) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.activeFetches = activeFetches
	}
}

func workerHeartbeat(
	workerID string,
	activeFetches func() uint32,
) *crawlrpc.WorkerHeartbeat {
	heartbeat := &crawlrpc.WorkerHeartbeat{WorkerId: workerID}
	if activeFetches != nil {
		active := activeFetches()
		heartbeat.ActiveFetches = &active
	}

	return heartbeat
}
