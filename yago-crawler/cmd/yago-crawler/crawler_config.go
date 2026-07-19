package main

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	EnvNodeRPCAddr                  = "YAGO_CRAWLER_NODE_RPC_ADDR"
	EnvDataDir                      = "YAGO_DATA_DIR"
	EnvWorkerID                     = "YAGO_CRAWLER_WORKER_ID"
	EnvWorkers                      = "YAGO_CRAWLER_WORKERS"
	EnvProcessPagesPerSecond        = "YAGO_CRAWLER_MAX_PAGES_PER_SECOND"
	EnvMaxActiveRuns                = "YAGO_CRAWLER_MAX_ACTIVE_RUNS"
	EnvPrioritizeAutomaticDiscovery = "YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY"
	EnvMaxHostConcurrency           = "YAGO_CRAWLER_MAX_HOST_CONCURRENCY"
	EnvMaxDepth                     = "YAGO_CRAWLER_MAX_DEPTH"
	EnvMaxPagesPerRun               = "YAGO_CRAWLER_MAX_PAGES_PER_RUN"
	EnvRunPagesPerMinute            = "YAGO_CRAWLER_RUN_PAGES_PER_MINUTE"
	EnvCrawlDelay                   = "YAGO_CRAWLER_CRAWL_DELAY"
	EnvUserAgent                    = "YAGO_CRAWLER_USER_AGENT"
	EnvRequestTimeout               = "YAGO_CRAWLER_REQUEST_TIMEOUT"
	EnvConnectTimeout               = "YAGO_CRAWLER_CONNECT_TIMEOUT"
	EnvTLSTimeout                   = "YAGO_CRAWLER_TLS_TIMEOUT"
	EnvHeaderTimeout                = "YAGO_CRAWLER_HEADER_TIMEOUT"
	EnvMaxRedirects                 = "YAGO_CRAWLER_MAX_REDIRECTS"
	EnvSitemapURLLimit              = "YAGO_CRAWLER_SITEMAP_URL_LIMIT"
	EnvEgressAllowLAN               = "YAGO_CRAWLER_ALLOW_PRIVATE_NETWORKS"
	EnvMetricsAddr                  = "YAGO_CRAWLER_METRICS_ADDR"
	EnvShutdownGrace                = "YAGO_CRAWLER_SHUTDOWN_GRACE"
	EnvBrowserPath                  = "YAGO_CRAWLER_BROWSER_PATH"
	EnvBrowserSandbox               = "YAGO_CRAWLER_BROWSER_SANDBOX"
	EnvBrowserFailureThreshold      = "YAGO_CRAWLER_BROWSER_FAILURE_THRESHOLD"
	EnvStorageReservedFree          = "YAGO_CRAWLER_STORAGE_RESERVED_FREE"
	EnvStoragePressureHysteresis    = "YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS"
	EnvFrontierStateMaximumBytes    = "YAGO_CRAWLER_FRONTIER_STATE_MAX_BYTES"

	DefaultWorkerID                         = "yago-crawler"
	DefaultDataDir                          = "./data"
	DefaultShutdownGrace                    = yagocrawlcontract.DefaultCrawlerShutdownGrace
	DefaultStorageReservedFree       uint64 = 1 << 30
	DefaultStoragePressureHysteresis uint64 = 256 << 20

	DefaultMaxBodyBytes       int64 = 4 << 20
	DefaultRequestTimeout           = yagocrawlcontract.DefaultCrawlerRequestTimeout
	DefaultConnectTimeout           = yagocrawlcontract.DefaultCrawlerConnectTimeout
	DefaultTLSTimeout               = yagocrawlcontract.DefaultCrawlerTLSTimeout
	DefaultHeaderTimeout            = yagocrawlcontract.DefaultCrawlerHeaderTimeout
	DefaultMaxRedirects             = yagocrawlcontract.DefaultMaximumPageRedirects
	DefaultMaxHostConcurrency       = yagocrawlcontract.DefaultCrawlerMaximumHostConcurrency
	DefaultSitemapURLLimit          = yagocrawlcontract.DefaultCrawlerSitemapURLLimit
	DefaultHostCacheSize            = 4096
	DefaultMaxPagesPerRun           = yagocrawlcontract.DefaultMaxPagesPerRun
	// DefaultRunPagesPerMinute paces every crawl run at a polite 30 fetches per
	// minute unless the operator raises (or zeroes) the run's rate explicitly,
	// so a fresh crawl cannot flatten the box or hammer the web at full speed.
	DefaultRunPagesPerMinute uint32 = yagocrawlcontract.DefaultCrawlerRunPagesPerMinute
)

// DefaultUserAgent brands crawl requests (plain HTTP and robots fetches alike)
// with the crawler's build version, so site operators can identify this crawler
// generation and find its contact page.
var DefaultUserAgent = "yago-crawler/" + version + " (+https://github.com/D4rk4/yago/)"

type CrawlConfig struct {
	Workers                      int
	ProcessPagesPerSecond        uint32
	MaxActiveRuns                int
	PrioritizeAutomaticDiscovery bool
	JobQueueSize                 int
	MaxBodyBytes                 int64
	RequestTimeout               time.Duration
	ConnectTimeout               time.Duration
	TLSTimeout                   time.Duration
	HeaderTimeout                time.Duration
	UserAgent                    string
	MaxRedirects                 int
	SitemapURLLimit              int
	CrawlDelay                   time.Duration
	MaxHostConcurrency           int
	MaxDepth                     int
	Scope                        yagocrawlcontract.CrawlScope
	MaxPagesPerHost              int
	MaxPagesPerRun               int
	RunPagesPerMinute            uint32
	HostCacheSize                int
	BrowserPath                  string
	BrowserSandbox               bool
	BrowserFailureThreshold      int
	redirectLimit                *redirectLimit
}

func DefaultCrawlConfig() CrawlConfig {
	return CrawlConfig{
		Workers:                      yagocrawlcontract.DefaultFetchWorkerConcurrency,
		ProcessPagesPerSecond:        yagocrawlcontract.DefaultProcessPagesPerSecond,
		MaxActiveRuns:                yagocrawlcontract.DefaultActiveCrawlRunConcurrency,
		PrioritizeAutomaticDiscovery: true,
		JobQueueSize:                 256,
		MaxBodyBytes:                 DefaultMaxBodyBytes,
		RequestTimeout:               DefaultRequestTimeout,
		ConnectTimeout:               DefaultConnectTimeout,
		TLSTimeout:                   DefaultTLSTimeout,
		HeaderTimeout:                DefaultHeaderTimeout,
		MaxRedirects:                 DefaultMaxRedirects,
		SitemapURLLimit:              DefaultSitemapURLLimit,
		UserAgent:                    DefaultUserAgent,
		CrawlDelay:                   yagocrawlcontract.DefaultCrawlerCrawlDelay,
		MaxHostConcurrency:           DefaultMaxHostConcurrency,
		MaxDepth:                     yagocrawlcontract.DefaultCrawlerMaximumDepth,
		Scope:                        yagocrawlcontract.ScopeDomain,
		MaxPagesPerHost:              yagocrawlcontract.UnlimitedPagesPerHost,
		MaxPagesPerRun:               DefaultMaxPagesPerRun,
		RunPagesPerMinute:            DefaultRunPagesPerMinute,
		HostCacheSize:                DefaultHostCacheSize,
		BrowserFailureThreshold:      yagocrawlcontract.DefaultCrawlerBrowserFailureThreshold,
	}
}

type ServiceConfig struct {
	Crawl                          CrawlConfig
	NodeRPCAddr                    string
	DataDir                        string
	WorkerID                       string
	MetricsAddr                    string
	ShutdownGrace                  time.Duration
	EgressAllowLAN                 bool
	EgressAllowedCIDRs             []netip.Prefix
	StorageReservedFreeBytes       uint64
	StoragePressureHysteresisBytes uint64
	FrontierStateMaximumBytes      uint64
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	nodeAddr := strings.TrimSpace(getenv(EnvNodeRPCAddr))
	if nodeAddr == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvNodeRPCAddr)
	}

	egressAllowLAN, err := envBool(getenv, EnvEgressAllowLAN, false)
	if err != nil {
		return ServiceConfig{}, err
	}

	egressAllowedCIDRs, err := parseEgressAllowCIDRs(getenv(EnvEgressAllowCIDRs))
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("%s: %w", EnvEgressAllowCIDRs, err)
	}

	crawl, err := loadCrawlConfig(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	shutdownGrace, err := envPositiveDuration(getenv, EnvShutdownGrace, DefaultShutdownGrace)
	if err != nil {
		return ServiceConfig{}, err
	}
	storageReservedFree, err := envByteSize(
		getenv,
		EnvStorageReservedFree,
		DefaultStorageReservedFree,
	)
	if err != nil {
		return ServiceConfig{}, err
	}
	storagePressureHysteresis, err := envByteSize(
		getenv,
		EnvStoragePressureHysteresis,
		DefaultStoragePressureHysteresis,
	)
	if err != nil {
		return ServiceConfig{}, err
	}
	frontierStateMaximumBytes, err := envByteSize(
		getenv,
		EnvFrontierStateMaximumBytes,
		yagocrawlcontract.DefaultCrawlerFrontierStateMaximumBytes,
	)
	if err != nil {
		return ServiceConfig{}, err
	}
	metricsAddress, err := yagocrawlcontract.ParseCrawlerMetricsAddress(
		getenv(EnvMetricsAddr),
	)
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("%s: %w", EnvMetricsAddr, err)
	}
	workerID, err := loadCrawlerWorkerIdentityPrefix(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	config := ServiceConfig{
		Crawl:                          crawl,
		NodeRPCAddr:                    nodeAddr,
		DataDir:                        envString(getenv, EnvDataDir, DefaultDataDir),
		WorkerID:                       workerID,
		MetricsAddr:                    metricsAddress,
		ShutdownGrace:                  shutdownGrace,
		EgressAllowLAN:                 egressAllowLAN,
		EgressAllowedCIDRs:             egressAllowedCIDRs,
		StorageReservedFreeBytes:       storageReservedFree,
		StoragePressureHysteresisBytes: storagePressureHysteresis,
		FrontierStateMaximumBytes:      frontierStateMaximumBytes,
	}
	if err := config.runtimePolicy().Validate(); err != nil {
		return ServiceConfig{}, fmt.Errorf("crawler runtime policy: %w", err)
	}

	return config, nil
}

func loadCrawlConfig(getenv func(string) string) (CrawlConfig, error) {
	crawl := DefaultCrawlConfig()

	crawl, err := loadCrawlSchedulerConfig(getenv, crawl)
	if err != nil {
		return CrawlConfig{}, err
	}

	crawl, err = loadCrawlLimits(getenv, crawl)
	if err != nil {
		return CrawlConfig{}, err
	}

	delay, err := envDuration(getenv, EnvCrawlDelay, crawl.CrawlDelay)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.CrawlDelay = delay

	crawl.UserAgent = envString(getenv, EnvUserAgent, crawl.UserAgent)

	requestTimeout, err := envPositiveDuration(getenv, EnvRequestTimeout, crawl.RequestTimeout)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.RequestTimeout = requestTimeout

	connectTimeout, err := envPositiveDuration(getenv, EnvConnectTimeout, crawl.ConnectTimeout)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.ConnectTimeout = connectTimeout

	tlsTimeout, err := envPositiveDuration(getenv, EnvTLSTimeout, crawl.TLSTimeout)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.TLSTimeout = tlsTimeout

	headerTimeout, err := envPositiveDuration(getenv, EnvHeaderTimeout, crawl.HeaderTimeout)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.HeaderTimeout = headerTimeout

	redirects, err := envNonNegativeInt(getenv, EnvMaxRedirects, crawl.MaxRedirects)
	if err != nil {
		return CrawlConfig{}, err
	}
	if redirects > yagocrawlcontract.MaximumPageRedirects {
		return CrawlConfig{}, fmt.Errorf(
			"%s: must not exceed %d",
			EnvMaxRedirects,
			yagocrawlcontract.MaximumPageRedirects,
		)
	}
	crawl.MaxRedirects = redirects

	sitemapURLLimit, err := envPositiveInt(getenv, EnvSitemapURLLimit, crawl.SitemapURLLimit)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.SitemapURLLimit = sitemapURLLimit

	crawl, err = loadBrowserConfig(getenv, crawl)
	if err != nil {
		return CrawlConfig{}, err
	}

	return crawl, nil
}

// loadBrowserConfig folds the slow-path browser settings — the binary path, the
// content sandbox toggle, and the fallback circuit-breaker's failure threshold —
// into crawl.
func loadBrowserConfig(getenv func(string) string, crawl CrawlConfig) (CrawlConfig, error) {
	browserPath, err := yagocrawlcontract.ParseCrawlerBrowserPath(getenv(EnvBrowserPath))
	if err != nil {
		return CrawlConfig{}, fmt.Errorf("%s: %w", EnvBrowserPath, err)
	}
	crawl.BrowserPath = browserPath

	browserSandbox, err := envBool(getenv, EnvBrowserSandbox, crawl.BrowserSandbox)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.BrowserSandbox = browserSandbox

	threshold, err := envNonNegativeInt(
		getenv, EnvBrowserFailureThreshold, crawl.BrowserFailureThreshold,
	)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.BrowserFailureThreshold = threshold

	return crawl, nil
}

// loadCrawlLimits reads the bounds on how far and how much one crawl run fetches:
// the maximum link depth and the whole-run page budget.
func loadCrawlLimits(getenv func(string) string, crawl CrawlConfig) (CrawlConfig, error) {
	depth, err := envPositiveInt(getenv, EnvMaxDepth, crawl.MaxDepth)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.MaxDepth = depth

	maxPagesPerRun, err := envNonNegativeInt(getenv, EnvMaxPagesPerRun, crawl.MaxPagesPerRun)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.MaxPagesPerRun = maxPagesPerRun

	if raw := strings.TrimSpace(getenv(EnvRunPagesPerMinute)); raw != "" {
		crawl.RunPagesPerMinute, err = yagocrawlcontract.ParseCrawlerRunPagesPerMinute(raw)
		if err != nil {
			return CrawlConfig{}, fmt.Errorf("%s: %w", EnvRunPagesPerMinute, err)
		}
	}

	return crawl, nil
}

func envBool(getenv func(string) string, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func envString(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envPositiveInt(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return value, nil
}

func envPositiveDuration(
	getenv func(string) string,
	key string,
	fallback time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return value, nil
}

func envNonNegativeInt(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s: must not be negative", key)
	}
	return value, nil
}

func envDuration(
	getenv func(string) string,
	key string,
	fallback time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s: must not be negative", key)
	}
	return value, nil
}
