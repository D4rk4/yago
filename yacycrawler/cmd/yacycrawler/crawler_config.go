package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawldelay"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlorder"
)

const (
	EnvNATSURL                     = "NATS_URL"
	EnvNATSOrdersSubject           = "NATS_ORDERS_SUBJECT"
	EnvNATSCrawledPageIndexSubject = "NATS_CRAWLED_PAGE_INDEX_SUBJECT"
	EnvNATSCrawledPageIndexMaxMsgs = "NATS_CRAWLED_PAGE_INDEX_MAX_MSGS"
	EnvNATSOrdersDurable           = "NATS_ORDERS_DURABLE"
	EnvNATSOrdersAckWait           = "NATS_ORDERS_ACK_WAIT"
	EnvNATSOrdersMaxDeliver        = "NATS_ORDERS_MAX_DELIVER"
	EnvWorkers                     = "YACYCRAWLER_WORKERS"
	EnvMaxDepth                    = "YACYCRAWLER_MAX_DEPTH"
	EnvCrawlDelay                  = "YACYCRAWLER_CRAWL_DELAY"
	EnvUserAgent                   = "YACYCRAWLER_USER_AGENT"
	EnvProxyURL                    = "YACYCRAWLER_PROXY_URL"

	EnvCrawledPageEnabled     = "YACYCRAWLER_CRAWLED_PAGE_ENABLED"
	EnvNATSCrawledPageSubject = "NATS_CRAWLED_PAGE_SUBJECT"
	EnvNATSCrawledPageMaxMsgs = "NATS_CRAWLED_PAGE_MAX_MSGS"

	DefaultOrdersSubject           = "yacy.crawl.orders"
	DefaultCrawledPageIndexSubject = "yacy.crawl.page-index"
	DefaultOrdersDurable           = "yacy-crawlers"
	DefaultOrdersAckWait           = 30 * time.Second
	DefaultOrdersMaxDeliver        = 5
	DefaultCrawledPageIndexMaxMsgs = 1024
	DefaultCrawledPageSubject      = "yacy.crawl.pages"
	DefaultCrawledPageMaxMsgs      = 1024

	DefaultMaxBodyBytes  int64 = 4 << 20
	DefaultUserAgent           = "yacy-rwi-node-crawler/0.1 (+https://yacy.net)"
	DefaultHostCacheSize       = 4096
)

type CrawlConfig struct {
	Workers         int
	JobQueueSize    int
	MaxBodyBytes    int64
	RequestTimeout  time.Duration
	UserAgent       string
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
		RequestTimeout:  15 * time.Second,
		UserAgent:       DefaultUserAgent,
		CrawlDelay:      crawldelay.DefaultCrawlDelay,
		MaxDepth:        2,
		Scope:           yacycrawlcontract.ScopeDomain,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		HostCacheSize:   DefaultHostCacheSize,
	}
}

type ServiceConfig struct {
	Crawl                   CrawlConfig
	NATSURL                 string
	ProxyURL                *url.URL
	OrdersSubject           string
	CrawledPageIndexSubject string
	OrdersDurable           string
	OrdersRedelivery        crawlorder.OrderRedeliveryPolicy
	CrawledPageIndexMaxMsgs int64
	CrawledPageEnabled      bool
	CrawledPageSubject      string
	CrawledPageMaxMsgs      int64
}

func (c ServiceConfig) OrdersStreamSpec() yacycrawlcontract.OrdersStreamSpec {
	return yacycrawlcontract.OrdersStreamSpec{Subject: c.OrdersSubject}
}

func (c ServiceConfig) CrawledPageIndexStreamSpec() yacycrawlcontract.CrawledPageIndexStreamSpec {
	return yacycrawlcontract.CrawledPageIndexStreamSpec{
		Subject: c.CrawledPageIndexSubject,
		MaxMsgs: c.CrawledPageIndexMaxMsgs,
	}
}

func (c ServiceConfig) CrawledPageStreamSpec() yacycrawlcontract.CrawledPageStreamSpec {
	return yacycrawlcontract.CrawledPageStreamSpec{
		Subject: c.CrawledPageSubject,
		MaxMsgs: c.CrawledPageMaxMsgs,
	}
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	natsURL := strings.TrimSpace(getenv(EnvNATSURL))
	if natsURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvNATSURL)
	}

	proxyURL, err := egressProxyURL(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	crawl := DefaultCrawlConfig()

	workers, err := envPositiveInt(getenv, EnvWorkers, crawl.Workers)
	if err != nil {
		return ServiceConfig{}, err
	}
	crawl.Workers = workers

	depth, err := envPositiveInt(getenv, EnvMaxDepth, crawl.MaxDepth)
	if err != nil {
		return ServiceConfig{}, err
	}
	crawl.MaxDepth = depth

	delay, err := envDuration(getenv, EnvCrawlDelay, crawl.CrawlDelay)
	if err != nil {
		return ServiceConfig{}, err
	}
	crawl.CrawlDelay = delay

	crawl.UserAgent = envString(getenv, EnvUserAgent, crawl.UserAgent)

	crawledPageIndexMaxMsgs, err := envPositiveInt64(
		getenv,
		EnvNATSCrawledPageIndexMaxMsgs,
		DefaultCrawledPageIndexMaxMsgs,
	)
	if err != nil {
		return ServiceConfig{}, err
	}

	crawledPageMaxMsgs, err := envPositiveInt64(
		getenv,
		EnvNATSCrawledPageMaxMsgs,
		DefaultCrawledPageMaxMsgs,
	)
	if err != nil {
		return ServiceConfig{}, err
	}

	ordersRedelivery, err := ordersRedeliveryFromEnv(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		Crawl:         crawl,
		NATSURL:       natsURL,
		ProxyURL:      proxyURL,
		OrdersSubject: envString(getenv, EnvNATSOrdersSubject, DefaultOrdersSubject),
		CrawledPageIndexSubject: envString(
			getenv,
			EnvNATSCrawledPageIndexSubject,
			DefaultCrawledPageIndexSubject,
		),
		OrdersDurable:           envString(getenv, EnvNATSOrdersDurable, DefaultOrdersDurable),
		OrdersRedelivery:        ordersRedelivery,
		CrawledPageIndexMaxMsgs: crawledPageIndexMaxMsgs,
		CrawledPageEnabled:      envBool(getenv, EnvCrawledPageEnabled, false),
		CrawledPageSubject: envString(
			getenv,
			EnvNATSCrawledPageSubject,
			DefaultCrawledPageSubject,
		),
		CrawledPageMaxMsgs: crawledPageMaxMsgs,
	}, nil
}

func ordersRedeliveryFromEnv(
	getenv func(string) string,
) (crawlorder.OrderRedeliveryPolicy, error) {
	ackWait, err := envDuration(getenv, EnvNATSOrdersAckWait, DefaultOrdersAckWait)
	if err != nil {
		return crawlorder.OrderRedeliveryPolicy{}, err
	}
	maxAttempts, err := envPositiveInt(getenv, EnvNATSOrdersMaxDeliver, DefaultOrdersMaxDeliver)
	if err != nil {
		return crawlorder.OrderRedeliveryPolicy{}, err
	}
	return crawlorder.OrderRedeliveryPolicy{AckWait: ackWait, MaxAttempts: maxAttempts}, nil
}

func envBool(getenv func(string) string, key string, fallback bool) bool {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
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
