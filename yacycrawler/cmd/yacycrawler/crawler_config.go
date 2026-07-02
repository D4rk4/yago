package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/crawldelay"
)

const (
	EnvNATSURL           = "NATS_URL"
	EnvNATSOrdersSubject = "NATS_ORDERS_SUBJECT"
	EnvNATSIngestSubject = "NATS_INGEST_SUBJECT"
	EnvNATSIngestMaxMsgs = "NATS_INGEST_MAX_MSGS"
	EnvNATSDurable       = "NATS_ORDERS_DURABLE"
	EnvWorkers           = "YACYCRAWLER_WORKERS"
	EnvMaxDepth          = "YACYCRAWLER_MAX_DEPTH"
	EnvCrawlDelay        = "YACYCRAWLER_CRAWL_DELAY"
	EnvUserAgent         = "YACYCRAWLER_USER_AGENT"
	EnvRequestTimeout    = "YACYCRAWLER_REQUEST_TIMEOUT"
	EnvConnectTimeout    = "YACYCRAWLER_CONNECT_TIMEOUT"
	EnvTLSTimeout        = "YACYCRAWLER_TLS_TIMEOUT"
	EnvHeaderTimeout     = "YACYCRAWLER_HEADER_TIMEOUT"
	EnvMaxRedirects      = "YACYCRAWLER_MAX_REDIRECTS"
	EnvSitemapURLLimit   = "YACYCRAWLER_SITEMAP_URL_LIMIT"
	EnvEgressAllowLAN    = "YACYCRAWLER_ALLOW_PRIVATE_NETWORKS"

	DefaultOrdersSubject = "yacy.crawl.orders"
	DefaultIngestSubject = "yacy.crawl.ingest"
	DefaultOrdersDurable = "yacy-crawlers"
	DefaultIngestMaxMsgs = 1024

	DefaultMaxBodyBytes    int64 = 4 << 20
	DefaultRequestTimeout        = 15 * time.Second
	DefaultConnectTimeout        = 5 * time.Second
	DefaultTLSTimeout            = 5 * time.Second
	DefaultHeaderTimeout         = 10 * time.Second
	DefaultMaxRedirects          = 10
	DefaultSitemapURLLimit       = 10000
	DefaultUserAgent             = "yago-crawler/0.1 (+https://github.com/D4rk4/yago/)"
	DefaultHostCacheSize         = 4096
)

type CrawlConfig struct {
	Workers         int
	JobQueueSize    int
	MaxBodyBytes    int64
	RequestTimeout  time.Duration
	ConnectTimeout  time.Duration
	TLSTimeout      time.Duration
	HeaderTimeout   time.Duration
	UserAgent       string
	MaxRedirects    int
	SitemapURLLimit int
	CrawlDelay      time.Duration
	MaxDepth        int
	Scope           yacycrawlcontract.CrawlScope
	MaxPagesPerHost int
	HostCacheSize   int
}

func DefaultCrawlConfig() CrawlConfig {
	return CrawlConfig{
		Workers:         4,
		JobQueueSize:    256,
		MaxBodyBytes:    DefaultMaxBodyBytes,
		RequestTimeout:  DefaultRequestTimeout,
		ConnectTimeout:  DefaultConnectTimeout,
		TLSTimeout:      DefaultTLSTimeout,
		HeaderTimeout:   DefaultHeaderTimeout,
		MaxRedirects:    DefaultMaxRedirects,
		SitemapURLLimit: DefaultSitemapURLLimit,
		UserAgent:       DefaultUserAgent,
		CrawlDelay:      crawldelay.DefaultCrawlDelay,
		MaxDepth:        2,
		Scope:           yacycrawlcontract.ScopeDomain,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		HostCacheSize:   DefaultHostCacheSize,
	}
}

type ServiceConfig struct {
	Crawl          CrawlConfig
	NATSURL        string
	EgressAllowLAN bool
	OrdersSubject  string
	IngestSubject  string
	OrdersDurable  string
	IngestMaxMsgs  int64
}

func (c ServiceConfig) StreamSpec() yacycrawlcontract.StreamSpec {
	return yacycrawlcontract.StreamSpec{
		OrdersSubject: c.OrdersSubject,
		IngestSubject: c.IngestSubject,
		IngestMaxMsgs: c.IngestMaxMsgs,
	}
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	natsURL := strings.TrimSpace(getenv(EnvNATSURL))
	if natsURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvNATSURL)
	}

	egressAllowLAN, err := envBool(getenv, EnvEgressAllowLAN, false)
	if err != nil {
		return ServiceConfig{}, err
	}

	crawl, err := loadCrawlConfig(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	maxMsgs, err := envPositiveInt64(getenv, EnvNATSIngestMaxMsgs, DefaultIngestMaxMsgs)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		Crawl:          crawl,
		NATSURL:        natsURL,
		EgressAllowLAN: egressAllowLAN,
		OrdersSubject:  envString(getenv, EnvNATSOrdersSubject, DefaultOrdersSubject),
		IngestSubject:  envString(getenv, EnvNATSIngestSubject, DefaultIngestSubject),
		OrdersDurable:  envString(getenv, EnvNATSDurable, DefaultOrdersDurable),
		IngestMaxMsgs:  maxMsgs,
	}, nil
}

func loadCrawlConfig(getenv func(string) string) (CrawlConfig, error) {
	crawl := DefaultCrawlConfig()

	workers, err := envPositiveInt(getenv, EnvWorkers, crawl.Workers)
	if err != nil {
		return CrawlConfig{}, err
	}
	crawl.Workers = workers

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

func envPositiveInt64(getenv func(string) string, key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
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
