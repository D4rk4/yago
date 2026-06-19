package yacycrawler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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

	DefaultOrdersSubject = "yacy.crawl.orders"
	DefaultIngestSubject = "yacy.crawl.ingest"
	DefaultOrdersDurable = "yacy-crawlers"
	DefaultIngestMaxMsgs = 1024
)

type ServiceConfig struct {
	Crawl         CrawlConfig
	NATSURL       string
	OrdersSubject string
	IngestSubject string
	OrdersDurable string
	IngestMaxMsgs int64
}

func (c ServiceConfig) StreamSpec() StreamSpec {
	return StreamSpec{
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

	maxMsgs, err := envPositiveInt64(getenv, EnvNATSIngestMaxMsgs, DefaultIngestMaxMsgs)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		Crawl:         crawl,
		NATSURL:       natsURL,
		OrdersSubject: envString(getenv, EnvNATSOrdersSubject, DefaultOrdersSubject),
		IngestSubject: envString(getenv, EnvNATSIngestSubject, DefaultIngestSubject),
		OrdersDurable: envString(getenv, EnvNATSDurable, DefaultOrdersDurable),
		IngestMaxMsgs: maxMsgs,
	}, nil
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
