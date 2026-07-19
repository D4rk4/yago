package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	settingKeyStorageReservedFree        = "storage.reserved_free"
	settingKeyStoragePressureHysteresis  = "storage.pressure_hysteresis"
	settingKeyCrawlerStorageReservedFree = "crawler.storage_reserved_free"
	settingKeyCrawlerStorageHysteresis   = "crawler.storage_pressure_hysteresis"
)

func storagePressureDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyStorageReservedFree,
			title:       "Node reserved free storage",
			description: "Pause gate-managed crawl and index ingestion when filesystem free space reaches this reserve. Deletion can leave reusable bbolt pages without restoring filesystem free space; free space or lower the reserve and recovery hysteresis to resume.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.StorageReservedFreeBytes)
			},
			normalize: normalizeStoragePressureSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.StorageReservedFreeBytes, _ = parseByteSize(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				bytes, _ := parseByteSize(value)
				toggles.ApplyStorageReservedFree(bytes)
			},
		},
		{
			key:         settingKeyStoragePressureHysteresis,
			title:       "Node storage recovery hysteresis",
			description: "Additional filesystem free space required above the reserve before gate-managed node ingestion resumes.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.StoragePressureRecoveryBytes)
			},
			normalize: normalizeStoragePressureSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.StoragePressureRecoveryBytes, _ = parseByteSize(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				bytes, _ := parseByteSize(value)
				toggles.ApplyStoragePressureRecovery(bytes)
			},
		},
		{
			key:         settingKeyCrawlerStorageReservedFree,
			title:       "Crawler reserved free storage",
			description: "Pause gate-managed crawler frontier growth and fetch admission when each crawler filesystem reaches this reserve. Deletion can leave reusable bbolt pages without restoring filesystem free space.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.Crawl.StorageReservedFreeBytes)
			},
			normalize: normalizeStoragePressureSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.StorageReservedFreeBytes, _ = parseByteSize(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				bytes, _ := parseByteSize(value)
				toggles.ApplyCrawlerStorageReservedFree(bytes)
			},
		},
		{
			key:         settingKeyCrawlerStorageHysteresis,
			title:       "Crawler storage recovery hysteresis",
			description: "Additional free space required above the reserve before crawler frontier growth and fetch admission resume.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.Crawl.StoragePressureRecoveryBytes)
			},
			normalize: normalizeStoragePressureSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.StoragePressureRecoveryBytes, _ = parseByteSize(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				bytes, _ := parseByteSize(value)
				toggles.ApplyCrawlerStoragePressureRecovery(bytes)
			},
		},
	}
}

func normalizeStoragePressureSize(raw string) (string, error) {
	bytes, err := parseByteSize(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("value must be zero or a non-negative byte size with a unit")
	}

	return formatByteSize(bytes), nil
}

func nodeStoragePressurePolicy(config nodeConfig) yagocrawlcontract.StoragePressurePolicy {
	return storagePressurePolicy(
		config.StorageReservedFreeBytes,
		config.StoragePressureRecoveryBytes,
	)
}

func crawlerStoragePressurePolicy(config crawlConfig) yagocrawlcontract.StoragePressurePolicy {
	return storagePressurePolicy(
		config.StorageReservedFreeBytes,
		config.StoragePressureRecoveryBytes,
	)
}

func storagePressurePolicy(reservedFree, recovery int64) yagocrawlcontract.StoragePressurePolicy {
	return yagocrawlcontract.StoragePressurePolicy{
		ReservedFreeBytes:       uint64(max(reservedFree, 0)),
		RecoveryHysteresisBytes: uint64(max(recovery, 0)),
	}
}
