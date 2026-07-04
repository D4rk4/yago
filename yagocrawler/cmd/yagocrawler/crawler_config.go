package main

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawldelay"
)

const (
	EnvNodeRPCAddr        = "YAGOCRAWLER_NODE_RPC_ADDR"
	EnvWorkerID           = "YAGOCRAWLER_WORKER_ID"
	EnvWorkers            = "YAGOCRAWLER_WORKERS"
	EnvMaxHostConcurrency = "YAGOCRAWLER_MAX_HOST_CONCURRENCY"
	EnvMaxDepth           = "YAGOCRAWLER_MAX_DEPTH"
	EnvCrawlDelay         = "YAGOCRAWLER_CRAWL_DELAY"
	EnvUserAgent          = "YAGOCRAWLER_USER_AGENT"
	EnvRequestTimeout     = "YAGOCRAWLER_REQUEST_TIMEOUT"
	EnvConnectTimeout     = "YAGOCRAWLER_CONNECT_TIMEOUT"
	EnvTLSTimeout         = "YAGOCRAWLER_TLS_TIMEOUT"
	EnvHeaderTimeout      = "YAGOCRAWLER_HEADER_TIMEOUT"
	EnvMaxRedirects       = "YAGOCRAWLER_MAX_REDIRECTS"
	EnvSitemapURLLimit    = "YAGOCRAWLER_SITEMAP_URL_LIMIT"
	EnvEgressAllowLAN     = "YAGOCRAWLER_ALLOW_PRIVATE_NETWORKS"
	EnvMetricsAddr        = "YAGOCRAWLER_METRICS_ADDR"
	EnvShutdownGrace      = "YAGOCRAWLER_SHUTDOWN_GRACE"

	DefaultWorkerID      = "yagocrawler"
	DefaultShutdownGrace = 10 * time.Second

	DefaultMaxBodyBytes       int64 = 4 << 20
	DefaultRequestTimeout           = 15 * time.Second
	DefaultConnectTimeout           = 5 * time.Second
	DefaultTLSTimeout               = 5 * time.Second
	DefaultHeaderTimeout            = 10 * time.Second
	DefaultMaxRedirects             = 10
	DefaultMaxHostConcurrency       = 2
	DefaultSitemapURLLimit          = 10000
	DefaultUserAgent                = "yago-crawler/0.1 (+https://github.com/D4rk4/yago/)"
	DefaultHostCacheSize            = 4096
)

type CrawlConfig struct {
	Workers            int
	JobQueueSize       int
	MaxBodyBytes       int64
	RequestTimeout     time.Duration
	ConnectTimeout     time.Duration
	TLSTimeout         time.Duration
	HeaderTimeout      time.Duration
	UserAgent          string
	MaxRedirects       int
	SitemapURLLimit    int
	CrawlDelay         time.Duration
	MaxHostConcurrency int
	MaxDepth           int
	Scope              yagocrawlcontract.CrawlScope
	MaxPagesPerHost    int
	HostCacheSize      int
}

func DefaultCrawlConfig() CrawlConfig {
	return CrawlConfig{
		Workers:            4,
		JobQueueSize:       256,
		MaxBodyBytes:       DefaultMaxBodyBytes,
		RequestTimeout:     DefaultRequestTimeout,
		ConnectTimeout:     DefaultConnectTimeout,
		TLSTimeout:         DefaultTLSTimeout,
		HeaderTimeout:      DefaultHeaderTimeout,
		MaxRedirects:       DefaultMaxRedirects,
		SitemapURLLimit:    DefaultSitemapURLLimit,
		UserAgent:          DefaultUserAgent,
		CrawlDelay:         crawldelay.DefaultCrawlDelay,
		MaxHostConcurrency: DefaultMaxHostConcurrency,
		MaxDepth:           2,
		Scope:              yagocrawlcontract.ScopeDomain,
		MaxPagesPerHost:    yagocrawlcontract.UnlimitedPagesPerHost,
		HostCacheSize:      DefaultHostCacheSize,
	}
}

type ServiceConfig struct {
	Crawl              CrawlConfig
	NodeRPCAddr        string
	WorkerID           string
	MetricsAddr        string
	ShutdownGrace      time.Duration
	EgressAllowLAN     bool
	EgressAllowedCIDRs []netip.Prefix
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

	return ServiceConfig{
		Crawl:              crawl,
		NodeRPCAddr:        nodeAddr,
		WorkerID:           envString(getenv, EnvWorkerID, DefaultWorkerID),
		MetricsAddr:        strings.TrimSpace(getenv(EnvMetricsAddr)),
		ShutdownGrace:      shutdownGrace,
		EgressAllowLAN:     egressAllowLAN,
		EgressAllowedCIDRs: egressAllowedCIDRs,
	}, nil
}

func loadCrawlConfig(getenv func(string) string) (CrawlConfig, error) {
	crawl := DefaultCrawlConfig()

	workers, err := envPositiveInt(getenv, EnvWorkers, crawl.Workers)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.Workers = workers

	maxHostConcurrency, err := envPositiveInt(
		getenv,
		EnvMaxHostConcurrency,
		crawl.MaxHostConcurrency,
	)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.MaxHostConcurrency = maxHostConcurrency

	depth, err := envPositiveInt(getenv, EnvMaxDepth, crawl.MaxDepth)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.MaxDepth = depth

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
	crawl.MaxRedirects = redirects

	sitemapURLLimit, err := envPositiveInt(getenv, EnvSitemapURLLimit, crawl.SitemapURLLimit)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.SitemapURLLimit = sitemapURLLimit

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
