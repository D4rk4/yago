package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	EnvNATSURL                = "NATS_URL"
	EnvNATSCrawledPageSubject = "NATS_CRAWLED_PAGE_SUBJECT"
	EnvNATSCrawledPageMaxMsgs = "NATS_CRAWLED_PAGE_MAX_MSGS"
	EnvNATSCrawledPageDurable = "NATS_CRAWLED_PAGE_DURABLE"
	EnvConcurrency            = "YACYTEXTINDEXER_CONCURRENCY"
	EnvSearchIndexEngine      = "SEARCH_INDEX_ENGINE"
	EnvElasticsearchURL       = "ELASTICSEARCH_URL"
	EnvElasticsearchIndex     = "ELASTICSEARCH_INDEX"
	EnvManticoreURL           = "MANTICORE_URL"
	EnvManticoreTable         = "MANTICORE_TABLE"

	DefaultCrawledPageSubject = "yacy.crawl.pages"
	DefaultCrawledPageMaxMsgs = 1024
	DefaultCrawledPageDurable = "yacytextindexer"
	DefaultConcurrency        = 4
	DefaultElasticsearchIndex = "yacy-text"
	DefaultManticoreTable     = "yacy_text"

	SearchIndexEngineElasticsearch = "elasticsearch"
	SearchIndexEngineManticore     = "manticore"
)

type ServiceConfig struct {
	NATSURL            string
	CrawledPageSubject string
	CrawledPageMaxMsgs int64
	CrawledPageDurable string
	Concurrency        int
	SearchIndexEngine  string
	ElasticsearchURL   string
	ElasticsearchIndex string
	ManticoreURL       string
	ManticoreTable     string
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

	maxMsgs, err := envPositiveInt64(getenv, EnvNATSCrawledPageMaxMsgs, DefaultCrawledPageMaxMsgs)
	if err != nil {
		return ServiceConfig{}, err
	}
	concurrency, err := envPositiveInt(getenv, EnvConcurrency, DefaultConcurrency)
	if err != nil {
		return ServiceConfig{}, err
	}

	cfg := ServiceConfig{
		NATSURL:            natsURL,
		CrawledPageSubject: envString(getenv, EnvNATSCrawledPageSubject, DefaultCrawledPageSubject),
		CrawledPageMaxMsgs: maxMsgs,
		CrawledPageDurable: envString(getenv, EnvNATSCrawledPageDurable, DefaultCrawledPageDurable),
		Concurrency:        concurrency,
		SearchIndexEngine:  strings.TrimSpace(getenv(EnvSearchIndexEngine)),
	}
	if cfg.SearchIndexEngine == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvSearchIndexEngine)
	}

	switch cfg.SearchIndexEngine {
	case SearchIndexEngineElasticsearch:
		cfg.ElasticsearchURL = strings.TrimSpace(getenv(EnvElasticsearchURL))
		if cfg.ElasticsearchURL == "" {
			return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvElasticsearchURL)
		}
		cfg.ElasticsearchIndex = envString(getenv, EnvElasticsearchIndex, DefaultElasticsearchIndex)
	case SearchIndexEngineManticore:
		cfg.ManticoreURL = strings.TrimSpace(getenv(EnvManticoreURL))
		if cfg.ManticoreURL == "" {
			return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvManticoreURL)
		}
		cfg.ManticoreTable = envString(getenv, EnvManticoreTable, DefaultManticoreTable)
	default:
		return ServiceConfig{}, fmt.Errorf(
			"%s: unknown engine %q", EnvSearchIndexEngine, cfg.SearchIndexEngine,
		)
	}

	return cfg, nil
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
