package yagocrawlcontract

import (
	"fmt"
	"strconv"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func automaticDiscoveryExecutionLimitsToProto(
	limits []AutomaticDiscoveryExecutionLimit,
) []*crawlrpc.AutomaticDiscoveryExecutionLimit {
	messages := make([]*crawlrpc.AutomaticDiscoveryExecutionLimit, len(limits))
	for index, limit := range limits {
		messages[index] = &crawlrpc.AutomaticDiscoveryExecutionLimit{
			ProfileName:         limit.ProfileName,
			MaximumDepth:        crawlerRuntimePolicyUint32(limit.MaximumDepth),
			MaximumPagesPerHost: crawlerRuntimePolicyUint64(limit.MaximumPagesPerHost),
			MaximumPagesPerRun:  crawlerRuntimePolicyUint64(limit.MaximumPagesPerRun),
		}
	}

	return messages
}

func automaticDiscoveryExecutionLimitsFromProto(
	messages []*crawlrpc.AutomaticDiscoveryExecutionLimit,
) ([]AutomaticDiscoveryExecutionLimit, error) {
	limits := make([]AutomaticDiscoveryExecutionLimit, len(messages))
	for index, message := range messages {
		if message == nil {
			return nil, fmt.Errorf("automatic discovery execution limit is missing")
		}
		maximumPagesPerHost, err := crawlerRuntimePolicyInt(
			"automatic discovery maximum pages per host",
			message.GetMaximumPagesPerHost(),
		)
		if err != nil {
			return nil, err
		}
		maximumPagesPerRun, err := crawlerRuntimePolicyInt(
			"automatic discovery maximum pages per run",
			message.GetMaximumPagesPerRun(),
		)
		if err != nil {
			return nil, err
		}
		limits[index] = AutomaticDiscoveryExecutionLimit{
			ProfileName:         message.GetProfileName(),
			MaximumDepth:        int(message.GetMaximumDepth()),
			MaximumPagesPerHost: maximumPagesPerHost,
			MaximumPagesPerRun:  maximumPagesPerRun,
		}
	}

	return limits, nil
}

func crawlerRuntimePolicyInt(name string, raw uint64) (int, error) {
	value, err := strconv.Atoi(strconv.FormatUint(raw, 10))
	if err != nil {
		return 0, fmt.Errorf("%s exceeds the supported range", name)
	}

	return value, nil
}
