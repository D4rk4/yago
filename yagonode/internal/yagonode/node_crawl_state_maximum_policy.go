package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const crawlerNodeStateQuotaDivisor = 10

type crawlStateMaximumPolicy struct {
	Automatic    bool
	MaximumBytes int64
}

func loadCrawlerNodeStateMaximum(getenv func(string) string) (crawlStateMaximumPolicy, error) {
	raw := strings.TrimSpace(getenv(envCrawlerNodeStateMaximumBytes))
	if raw == "" || strings.EqualFold(raw, "auto") {
		quotaBytes, err := parseByteSize(envWithDefault(getenv, envStorageQuota, defaultQuota))
		if err != nil {
			return crawlStateMaximumPolicy{}, fmt.Errorf("%s: %w", envStorageQuota, err)
		}

		return crawlStateMaximumPolicy{
			Automatic:    true,
			MaximumBytes: quotaBytes / crawlerNodeStateQuotaDivisor,
		}, nil
	}
	maximumBytes, err := parseByteSize(raw)
	if err != nil {
		return crawlStateMaximumPolicy{}, fmt.Errorf("%s: %w", envCrawlerNodeStateMaximumBytes, err)
	}

	return crawlStateMaximumPolicy{MaximumBytes: maximumBytes}, nil
}

func parseCrawlerNodeStateMaximum(raw string) (crawlStateMaximumPolicy, error) {
	value := strings.TrimSpace(raw)
	if strings.EqualFold(value, "auto") {
		return crawlStateMaximumPolicy{Automatic: true}, nil
	}
	maximumBytes, err := parseByteSize(value)
	if err != nil {
		return crawlStateMaximumPolicy{}, err
	}

	return crawlStateMaximumPolicy{MaximumBytes: maximumBytes}, nil
}

func normalizeCrawlerNodeStateMaximum(raw string) (string, error) {
	policy, err := parseCrawlerNodeStateMaximum(raw)
	if err != nil {
		return "", fmt.Errorf("value must be auto, zero, or a non-negative byte size with a unit")
	}
	if policy.Automatic {
		return "auto", nil
	}

	return formatByteSize(policy.MaximumBytes), nil
}

func crawlStateAutomaticMaximumBytes(
	quotaBytes int64,
	pressure *yagocrawlcontract.StoragePressureGate,
) int64 {
	if pressure == nil {
		return quotaBytes / crawlerNodeStateQuotaDivisor
	}

	return crawlStateAutomaticMaximum(quotaBytes, pressure.Snapshot())
}

func crawlStateAutomaticMaximum(
	quotaBytes int64,
	snapshot yagocrawlcontract.StoragePressureSnapshot,
) int64 {
	if !snapshot.MeasurementAvailable {
		return quotaBytes / crawlerNodeStateQuotaDivisor
	}
	diskBudget := uint64(0)
	if snapshot.AvailableBytes > snapshot.Policy.ReservedFreeBytes {
		diskBudget = snapshot.AvailableBytes - snapshot.Policy.ReservedFreeBytes
	}
	quotaBudget := uint64(0)
	if quotaBytes > 0 {
		quotaBudget = uint64(quotaBytes)
	}
	if quotaBudget == 0 {
		quotaBudget = diskBudget
	}
	maximumBytes := min(quotaBudget/crawlerNodeStateQuotaDivisor, diskBudget)

	return int64(maximumBytes)
}
