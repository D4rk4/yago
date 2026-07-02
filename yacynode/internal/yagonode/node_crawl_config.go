package yagonode

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	envNATSURL           = "NATS_URL"
	envNATSOrdersSubject = "NATS_ORDERS_SUBJECT"
	envNATSIngestSubject = "NATS_INGEST_SUBJECT"
	envNATSIngestDurable = "NATS_INGEST_DURABLE"
	envNATSIngestMaxMsgs = "NATS_INGEST_MAX_MSGS"

	defaultOrdersSubject = "yacy.crawl.orders"
	defaultIngestSubject = "yacy.crawl.ingest"
	defaultIngestDurable = "yacy-node"
	defaultIngestMaxMsgs = 1024
)

type crawlConfig struct {
	NATSURL       string
	OrdersSubject string
	IngestSubject string
	IngestDurable string
	IngestMaxMsgs int64
}

func (c crawlConfig) Enabled() bool {
	return c.NATSURL != ""
}

func loadCrawlConfig(getenv func(string) string) (crawlConfig, error) {
	url := strings.TrimSpace(getenv(envNATSURL))
	if url == "" {
		return crawlConfig{}, nil
	}

	maxMsgs := int64(defaultIngestMaxMsgs)
	if raw := strings.TrimSpace(getenv(envNATSIngestMaxMsgs)); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return crawlConfig{}, fmt.Errorf("%s: %w", envNATSIngestMaxMsgs, err)
		}
		if value <= 0 {
			return crawlConfig{}, fmt.Errorf("%s: must be positive", envNATSIngestMaxMsgs)
		}
		maxMsgs = value
	}

	return crawlConfig{
		NATSURL:       url,
		OrdersSubject: envWithDefault(getenv, envNATSOrdersSubject, defaultOrdersSubject),
		IngestSubject: envWithDefault(getenv, envNATSIngestSubject, defaultIngestSubject),
		IngestDurable: envWithDefault(getenv, envNATSIngestDurable, defaultIngestDurable),
		IngestMaxMsgs: maxMsgs,
	}, nil
}
