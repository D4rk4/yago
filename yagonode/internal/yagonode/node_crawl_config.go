package yagonode

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

const (
	envCrawlRPCAddr                 = "YAGO_CRAWL_RPC_ADDR"
	envCrawlerWorkers               = "YAGO_CRAWLER_WORKERS"
	envCrawlerMaxActiveRuns         = "YAGO_CRAWLER_MAX_ACTIVE_RUNS"
	envCrawlerMaxPagesPerRun        = "YAGO_CRAWLER_MAX_PAGES_PER_RUN"
	envPrioritizeAutomaticDiscovery = "YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY"
	envCrawlerStorageReservedFree   = "YAGO_CRAWLER_STORAGE_RESERVED_FREE"
	envCrawlerStorageHysteresis     = "YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS"
	defaultCrawlRPCAddr             = "127.0.0.1:9091"
	// crawlRPCDisabled is the explicit value that turns the crawl exchange off
	// for a node that runs no crawler, now that an unset value means "default".
	crawlRPCDisabled = "off"
)

type crawlConfig struct {
	ListenAddr                   string
	FetchWorkers                 int
	MaxActiveRuns                int
	MaxPagesPerRun               int
	PrioritizeAutomaticDiscovery bool
	StorageReservedFreeBytes     int64
	StoragePressureRecoveryBytes int64
	StatePath                    string
	// QualityGate rejects crawled pages that fail the deterministic Gopher/C4
	// content-quality rules before they are stored or indexed.
	QualityGate     bool
	GrowthAdmission crawlbroker.GrowthAdmission
}

func (c crawlConfig) Enabled() bool {
	return c.ListenAddr != ""
}

func loadCrawlConfig(getenv func(string) string) (crawlConfig, error) {
	qualityGate, err := boolEnv(getenv, envIngestQualityGate, true)
	if err != nil {
		return crawlConfig{}, fmt.Errorf("%s: %w", envIngestQualityGate, err)
	}
	fetchWorkers, err := intRangeEnv(
		getenv,
		envCrawlerWorkers,
		yagocrawlcontract.DefaultFetchWorkerConcurrency,
		1,
		yagocrawlcontract.MaximumFetchWorkerConcurrency,
	)
	if err != nil {
		return crawlConfig{}, err
	}
	maximumActiveRuns, err := intRangeEnv(
		getenv,
		envCrawlerMaxActiveRuns,
		yagocrawlcontract.DefaultActiveCrawlRunConcurrency,
		1,
		yagocrawlcontract.MaximumActiveCrawlRunConcurrency,
	)
	if err != nil {
		return crawlConfig{}, err
	}
	maxPagesPerRun, err := intAtLeastEnv(
		getenv,
		envCrawlerMaxPagesPerRun,
		yagocrawlcontract.DefaultMaxPagesPerRun,
		0,
	)
	if err != nil {
		return crawlConfig{}, err
	}
	prioritizeAutomaticDiscovery, err := boolEnv(
		getenv,
		envPrioritizeAutomaticDiscovery,
		true,
	)
	if err != nil {
		return crawlConfig{}, err
	}
	storageReservedFree, err := parseByteSize(envWithDefault(
		getenv,
		envCrawlerStorageReservedFree,
		defaultReservedFree,
	))
	if err != nil {
		return crawlConfig{}, fmt.Errorf("%s: %w", envCrawlerStorageReservedFree, err)
	}
	storagePressureRecovery, err := parseByteSize(envWithDefault(
		getenv,
		envCrawlerStorageHysteresis,
		defaultPressureRecovery,
	))
	if err != nil {
		return crawlConfig{}, fmt.Errorf("%s: %w", envCrawlerStorageHysteresis, err)
	}

	return crawlConfig{
		ListenAddr:                   crawlRPCListenAddr(getenv),
		FetchWorkers:                 fetchWorkers,
		MaxActiveRuns:                maximumActiveRuns,
		MaxPagesPerRun:               maxPagesPerRun,
		PrioritizeAutomaticDiscovery: prioritizeAutomaticDiscovery,
		StorageReservedFreeBytes:     storageReservedFree,
		StoragePressureRecoveryBytes: storagePressureRecovery,
		QualityGate:                  qualityGate,
	}, nil
}

func loadRuntimeCrawlConfig(
	getenv func(string) string,
	dataDirectory string,
) (crawlConfig, error) {
	config, err := loadCrawlConfig(getenv)
	if err != nil {
		return crawlConfig{}, err
	}
	config.StatePath = filepath.Join(dataDirectory, crawlBrokerStateFileName)

	return config, nil
}

// crawlRPCListenAddr resolves the crawl-exchange listen address: the operator's
// value when set, the loopback default when unset (so the co-located crawler
// connects out of the box), or empty — disabled — for the explicit "off"
// sentinel.
func crawlRPCListenAddr(getenv func(string) string) string {
	addr := strings.TrimSpace(getenv(envCrawlRPCAddr))
	switch {
	case addr == "":
		return defaultCrawlRPCAddr
	case strings.EqualFold(addr, crawlRPCDisabled):
		return ""
	default:
		return addr
	}
}
