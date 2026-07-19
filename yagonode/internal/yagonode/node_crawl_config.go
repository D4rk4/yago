package yagonode

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

const (
	envCrawlRPCAddr                     = "YAGO_CRAWL_RPC_ADDR"
	envCrawlerWorkers                   = "YAGO_CRAWLER_WORKERS"
	envCrawlerProcessPagesPerSecond     = "YAGO_CRAWLER_MAX_PAGES_PER_SECOND"
	envCrawlerMaximumRedirects          = "YAGO_CRAWLER_MAX_REDIRECTS"
	envCrawlerMaxActiveRuns             = "YAGO_CRAWLER_MAX_ACTIVE_RUNS"
	envCrawlerMaxPagesPerRun            = "YAGO_CRAWLER_MAX_PAGES_PER_RUN"
	envPrioritizeAutomaticDiscovery     = "YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY"
	envCrawlerStorageReservedFree       = "YAGO_CRAWLER_STORAGE_RESERVED_FREE"
	envCrawlerStorageHysteresis         = "YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS"
	envCrawlerNodeStateMaximumBytes     = "YAGO_CRAWLER_NODE_STATE_MAX_BYTES"
	envCrawlerAllowPrivateNetworks      = "YAGO_CRAWLER_ALLOW_PRIVATE_NETWORKS"
	envCrawlerAllowCIDRs                = "YAGO_CRAWLER_ALLOW_CIDRS"
	envCrawlerBrowserSandbox            = "YAGO_CRAWLER_BROWSER_SANDBOX"
	envCrawlerBrowserFailureLimit       = "YAGO_CRAWLER_BROWSER_FAILURE_THRESHOLD"
	envCrawlerBrowserPath               = "YAGO_CRAWLER_BROWSER_PATH"
	envCrawlerConnectTimeout            = "YAGO_CRAWLER_CONNECT_TIMEOUT"
	envCrawlerCrawlDelay                = "YAGO_CRAWLER_CRAWL_DELAY"
	envCrawlerHeaderTimeout             = "YAGO_CRAWLER_HEADER_TIMEOUT"
	envCrawlerMaximumDepth              = "YAGO_CRAWLER_MAX_DEPTH"
	envCrawlerMaximumHostFetches        = "YAGO_CRAWLER_MAX_HOST_CONCURRENCY"
	envCrawlerMetricsAddress            = "YAGO_CRAWLER_METRICS_ADDR"
	envCrawlerRequestTimeout            = "YAGO_CRAWLER_REQUEST_TIMEOUT"
	envCrawlerRunPagesPerMinute         = "YAGO_CRAWLER_RUN_PAGES_PER_MINUTE"
	envCrawlerSitemapURLLimit           = "YAGO_CRAWLER_SITEMAP_URL_LIMIT"
	envCrawlerTLSTimeout                = "YAGO_CRAWLER_TLS_TIMEOUT"
	envCrawlerShutdownGrace             = "YAGO_CRAWLER_SHUTDOWN_GRACE"
	envCrawlerUserAgent                 = "YAGO_CRAWLER_USER_AGENT"
	envCrawlerFrontierStateMaximumBytes = "YAGO_CRAWLER_FRONTIER_STATE_MAX_BYTES"
	defaultCrawlRPCAddr                 = "127.0.0.1:9091"
)

type crawlConfig struct {
	ListenAddr                   string
	FetchWorkers                 int
	ProcessPagesPerSecond        int
	MaximumRedirects             int
	MaxActiveRuns                int
	MaxPagesPerRun               int
	PrioritizeAutomaticDiscovery bool
	StorageReservedFreeBytes     int64
	StoragePressureRecoveryBytes int64
	StateMaximumBytes            int64
	RuntimePolicy                yagocrawlcontract.CrawlerRuntimePolicy
	StatePath                    string
	// QualityGate rejects crawled pages that fail the deterministic Gopher/C4
	// content-quality rules before they are stored or indexed.
	QualityGate     bool
	GrowthAdmission crawlbroker.GrowthAdmission
}

type crawlerStoragePressureConfig struct {
	reservedFreeBytes int64
	recoveryBytes     int64
}

type crawlerSchedulingConfig struct {
	maxPagesPerRun               int
	prioritizeAutomaticDiscovery bool
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
	processPagesPerSecond, err := intRangeEnv(
		getenv,
		envCrawlerProcessPagesPerSecond,
		yagocrawlcontract.DefaultProcessPagesPerSecond,
		0,
		yagocrawlcontract.MaximumProcessPagesPerSecond,
	)
	if err != nil {
		return crawlConfig{}, err
	}
	maximumRedirects, err := intRangeEnv(
		getenv,
		envCrawlerMaximumRedirects,
		yagocrawlcontract.DefaultMaximumPageRedirects,
		0,
		yagocrawlcontract.MaximumPageRedirects,
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
	scheduling, err := loadCrawlerSchedulingConfig(getenv)
	if err != nil {
		return crawlConfig{}, err
	}
	storagePressure, err := loadCrawlerStoragePressure(getenv)
	if err != nil {
		return crawlConfig{}, err
	}
	stateMaximumBytes, err := loadCrawlerNodeStateMaximum(getenv)
	if err != nil {
		return crawlConfig{}, err
	}
	runtimePolicy, err := loadCrawlerRuntimePolicy(getenv)
	if err != nil {
		return crawlConfig{}, err
	}

	return crawlConfig{
		ListenAddr:                   crawlRPCListenAddr(getenv),
		FetchWorkers:                 fetchWorkers,
		ProcessPagesPerSecond:        processPagesPerSecond,
		MaximumRedirects:             maximumRedirects,
		MaxActiveRuns:                maximumActiveRuns,
		MaxPagesPerRun:               scheduling.maxPagesPerRun,
		PrioritizeAutomaticDiscovery: scheduling.prioritizeAutomaticDiscovery,
		StorageReservedFreeBytes:     storagePressure.reservedFreeBytes,
		StoragePressureRecoveryBytes: storagePressure.recoveryBytes,
		StateMaximumBytes:            stateMaximumBytes,
		RuntimePolicy:                runtimePolicy,
		QualityGate:                  qualityGate,
	}, nil
}

func loadCrawlerSchedulingConfig(
	getenv func(string) string,
) (crawlerSchedulingConfig, error) {
	maxPagesPerRun, err := intAtLeastEnv(
		getenv,
		envCrawlerMaxPagesPerRun,
		yagocrawlcontract.DefaultMaxPagesPerRun,
		0,
	)
	if err != nil {
		return crawlerSchedulingConfig{}, err
	}
	prioritizeAutomaticDiscovery, err := boolEnv(
		getenv,
		envPrioritizeAutomaticDiscovery,
		true,
	)
	if err != nil {
		return crawlerSchedulingConfig{}, err
	}

	return crawlerSchedulingConfig{
		maxPagesPerRun:               maxPagesPerRun,
		prioritizeAutomaticDiscovery: prioritizeAutomaticDiscovery,
	}, nil
}

func loadCrawlerStoragePressure(
	getenv func(string) string,
) (crawlerStoragePressureConfig, error) {
	reservedFree, err := parseByteSize(envWithDefault(
		getenv,
		envCrawlerStorageReservedFree,
		defaultReservedFree,
	))
	if err != nil {
		return crawlerStoragePressureConfig{}, fmt.Errorf(
			"%s: %w",
			envCrawlerStorageReservedFree,
			err,
		)
	}
	recovery, err := parseByteSize(envWithDefault(
		getenv,
		envCrawlerStorageHysteresis,
		defaultPressureRecovery,
	))
	if err != nil {
		return crawlerStoragePressureConfig{}, fmt.Errorf(
			"%s: %w",
			envCrawlerStorageHysteresis,
			err,
		)
	}

	return crawlerStoragePressureConfig{
		reservedFreeBytes: reservedFree,
		recoveryBytes:     recovery,
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
	case strings.EqualFold(addr, disabledBindOverride):
		return ""
	default:
		return addr
	}
}
