package crawlbroker

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func crawlerStartupRuntimePolicy(
	runtimePolicy yagocrawlcontract.CrawlerRuntimePolicy,
	storagePolicy yagocrawlcontract.StoragePressurePolicy,
) (*crawlrpc.CrawlerRuntimePolicy, error) {
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(runtimePolicy)
	if err != nil {
		return nil, fmt.Errorf("encode crawler startup runtime policy: %w", err)
	}
	reservedFree := storagePolicy.ReservedFreeBytes
	pressureHysteresis := storagePolicy.RecoveryHysteresisBytes
	message.StorageReservedFreeBytes = &reservedFree
	message.StoragePressureHysteresisBytes = &pressureHysteresis

	return message, nil
}
