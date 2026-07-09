package main

import (
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/envconfig"
)

const (
	envNATSURL           = "NATS_URL"
	envNATSOrdersSubject = "NATS_ORDERS_SUBJECT"
	envNATSIngestSubject = "NATS_INGEST_SUBJECT"
	envNATSIngestDurable = "NATS_INGEST_DURABLE"
	envNATSIngestMaxMsgs = "NATS_INGEST_MAX_MSGS"

	defaultOrdersSubject = "yacy.crawl.orders"
	defaultIngestSubject = "yacy.crawl.page-index"
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

	maxMsgs, err := envconfig.PositiveInt64(getenv, envNATSIngestMaxMsgs, defaultIngestMaxMsgs)
	if err != nil {
		return crawlConfig{}, err
	}

	return crawlConfig{
		NATSURL:       url,
		OrdersSubject: envconfig.String(getenv, envNATSOrdersSubject, defaultOrdersSubject),
		IngestSubject: envconfig.String(getenv, envNATSIngestSubject, defaultIngestSubject),
		IngestDurable: envconfig.String(getenv, envNATSIngestDurable, defaultIngestDurable),
		IngestMaxMsgs: maxMsgs,
	}, nil
}
