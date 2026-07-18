package crawlorder

import (
	"context"
	"testing"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type storageHeartbeatClient struct {
	*fakeStreamer
	result *crawlrpc.WorkerHeartbeatResult
}

func (client *storageHeartbeatClient) Heartbeat(
	_ context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	client.mu.Lock()
	client.heartbeatCalls = append(client.heartbeatCalls, heartbeat)
	client.mu.Unlock()

	return client.result, nil
}

func TestHeartbeatExchangesStorageStatusAndLivePolicy(t *testing.T) {
	reservedFree := uint64(55)
	hysteresis := uint64(7)
	client := &storageHeartbeatClient{
		fakeStreamer: &fakeStreamer{ctx: t.Context()},
		result: &crawlrpc.WorkerHeartbeatResult{
			StorageReservedFreeBytes:       &reservedFree,
			StoragePressureHysteresisBytes: &hysteresis,
		},
	}
	var applied yagocrawlcontract.StoragePressurePolicy
	config := grpcOrderReceiverConfig{}
	WithHeartbeatStoragePressure(
		func() yagocrawlcontract.StoragePressureSnapshot {
			return yagocrawlcontract.StoragePressureSnapshot{
				AvailableBytes:       41,
				MeasurementAvailable: true,
				Pressured:            true,
			}
		},
		func(policy yagocrawlcontract.StoragePressurePolicy) {
			applied = policy
		},
	)(&config)
	delivery := heartbeatDelivery{
		client:          client,
		workerID:        "worker",
		storageSnapshot: config.storageSnapshot,
		storagePolicy:   config.storagePolicy,
	}
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("exchange heartbeat: %v", err)
	}
	requests := client.heartbeatRequests()
	if len(requests) != 1 || requests[0].StorageAvailableBytes == nil ||
		requests[0].StorageMeasurementAvailable == nil ||
		requests[0].StoragePressure == nil ||
		requests[0].GetStorageAvailableBytes() != 41 ||
		!requests[0].GetStorageMeasurementAvailable() ||
		!requests[0].GetStoragePressure() {
		t.Fatalf("storage heartbeat = %+v", requests)
	}
	if applied.ReservedFreeBytes != 55 || applied.RecoveryHysteresisBytes != 7 {
		t.Fatalf("applied storage policy = %+v", applied)
	}

	client.result = &crawlrpc.WorkerHeartbeatResult{}
	applied = yagocrawlcontract.StoragePressurePolicy{ReservedFreeBytes: 99}
	if _, err := delivery.exchange(t.Context(), nil); err != nil {
		t.Fatalf("exchange legacy heartbeat result: %v", err)
	}
	if applied.ReservedFreeBytes != 99 {
		t.Fatalf("legacy result replaced bootstrap policy: %+v", applied)
	}
}
