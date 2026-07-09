package main

import (
	"fmt"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/envconfig"
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
	EnvOpsAddr                = "YACYTEXTINDEXER_OPS_ADDR"

	DefaultOpsAddr            = ":9090"
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
	OpsAddr            string
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

	maxMsgs, err := envconfig.PositiveInt64(
		getenv,
		EnvNATSCrawledPageMaxMsgs,
		DefaultCrawledPageMaxMsgs,
	)
	if err != nil {
		return ServiceConfig{}, err
	}
	concurrency, err := envconfig.PositiveInt(getenv, EnvConcurrency, DefaultConcurrency)
	if err != nil {
		return ServiceConfig{}, err
	}

	cfg := ServiceConfig{
		NATSURL: natsURL,
		CrawledPageSubject: envconfig.String(
			getenv,
			EnvNATSCrawledPageSubject,
			DefaultCrawledPageSubject,
		),
		CrawledPageMaxMsgs: maxMsgs,
		CrawledPageDurable: envconfig.String(
			getenv,
			EnvNATSCrawledPageDurable,
			DefaultCrawledPageDurable,
		),
		Concurrency:       concurrency,
		SearchIndexEngine: strings.TrimSpace(getenv(EnvSearchIndexEngine)),
		OpsAddr:           envconfig.String(getenv, EnvOpsAddr, DefaultOpsAddr),
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
		cfg.ElasticsearchIndex = envconfig.String(
			getenv,
			EnvElasticsearchIndex,
			DefaultElasticsearchIndex,
		)
	case SearchIndexEngineManticore:
		cfg.ManticoreURL = strings.TrimSpace(getenv(EnvManticoreURL))
		if cfg.ManticoreURL == "" {
			return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvManticoreURL)
		}
		cfg.ManticoreTable = envconfig.String(getenv, EnvManticoreTable, DefaultManticoreTable)
	default:
		return ServiceConfig{}, fmt.Errorf(
			"%s: unknown engine %q", EnvSearchIndexEngine, cfg.SearchIndexEngine,
		)
	}

	return cfg, nil
}
