package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/envconfig"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/httpfetch"
)

const (
	EnvNATSURL            = "NATS_URL"
	EnvOrdersSubject      = "NATS_ORDERS_SUBJECT"
	EnvOrdersDurable      = "NATS_ORDERS_DURABLE"
	EnvPageIndexSubject   = "NATS_PAGE_INDEX_SUBJECT"
	EnvPageIndexMaxMsgs   = "NATS_PAGE_INDEX_MAX_MSGS"
	EnvPagesSubject       = "NATS_PAGES_SUBJECT"
	EnvPagesMaxMsgs       = "NATS_PAGES_MAX_MSGS"
	EnvProxyURL           = "YACYCRAWLER_PROXY_URL"
	EnvProxyDialMode      = "YACYCRAWLER_PROXY_DIAL_MODE"
	EnvFetchConcurrency   = "YACYCRAWLER_FETCH_CONCURRENCY"
	EnvIndexOutputEnabled = "YACYCRAWLER_INDEX_OUTPUT_ENABLED"
	EnvPageOutputEnabled  = "YACYCRAWLER_PAGE_OUTPUT_ENABLED"
	EnvRunPageBudget      = "YACYCRAWLER_RUN_PAGE_BUDGET"
	EnvFrontierCap        = "YACYCRAWLER_FRONTIER_CAP"
	EnvMaxBodyBytes       = "YACYCRAWLER_MAX_BODY_BYTES"
	EnvFetchDeadline      = "YACYCRAWLER_FETCH_DEADLINE"
	EnvContentTypes       = "YACYCRAWLER_CONTENT_TYPES"
	EnvOpsAddr            = "YACYCRAWLER_OPS_ADDR"
	EnvUserAgent          = "YACYCRAWLER_USER_AGENT"

	DefaultOrdersSubject    = "yacy.crawl.orders"
	DefaultOrdersDurable    = "yacycrawler"
	DefaultPageIndexSubject = "yacy.crawl.page-index"
	DefaultPagesSubject     = "yacy.crawl.pages"
	DefaultMaxMsgs          = 1024
	DefaultFetchConcurrency = 4
	DefaultRunPageBudget    = 1000
	DefaultFrontierCap      = 10000
	DefaultMaxBodyBytes     = 2 << 20
	DefaultFetchDeadline    = 30 * time.Second
	DefaultOpsAddr          = ":9090"
	DefaultUserAgent        = "yacycrawler (+https://yacy.net)"
	DefaultProxyDialMode    = "tunnel"
)

var proxyDialModeByName = map[string]httpfetch.ProxyDialMode{
	"tunnel":       httpfetch.ProxyDialTunnel,
	"absolute-url": httpfetch.ProxyDialAbsoluteURL,
}

type ServiceConfig struct {
	NATSURL            string
	OrdersSubject      string
	OrdersDurable      string
	PageIndexSubject   string
	PageIndexMaxMsgs   int64
	PagesSubject       string
	PagesMaxMsgs       int64
	ProxyURL           *url.URL
	ProxyDialMode      httpfetch.ProxyDialMode
	FetchConcurrency   int
	IndexOutputEnabled bool
	PageOutputEnabled  bool
	RunPageBudget      int
	FrontierCap        int
	MaxBodyBytes       int64
	FetchDeadline      time.Duration
	ContentTypes       []string
	OpsAddr            string
	UserAgent          string
}

func (c ServiceConfig) OrdersStreamSpec() yacycrawlcontract.OrdersStreamSpec {
	return yacycrawlcontract.OrdersStreamSpec{Subject: c.OrdersSubject}
}

func (c ServiceConfig) PageIndexStreamSpec() yacycrawlcontract.CrawledPageIndexStreamSpec {
	return yacycrawlcontract.CrawledPageIndexStreamSpec{
		Subject: c.PageIndexSubject,
		MaxMsgs: c.PageIndexMaxMsgs,
	}
}

func (c ServiceConfig) PagesStreamSpec() yacycrawlcontract.CrawledPageStreamSpec {
	return yacycrawlcontract.CrawledPageStreamSpec{
		Subject: c.PagesSubject,
		MaxMsgs: c.PagesMaxMsgs,
	}
}

type serviceLimits struct {
	pageIndexMaxMsgs int64
	pagesMaxMsgs     int64
	fetchConcurrency int
	runPageBudget    int
	frontierCap      int
	maxBodyBytes     int64
	fetchDeadline    time.Duration
	indexEnabled     bool
	pageEnabled      bool
}

func loadServiceLimits(getenv func(string) string) (serviceLimits, error) {
	pageIndexMaxMsgs, err := envconfig.PositiveInt64(getenv, EnvPageIndexMaxMsgs, DefaultMaxMsgs)
	if err != nil {
		return serviceLimits{}, err
	}
	pagesMaxMsgs, err := envconfig.PositiveInt64(getenv, EnvPagesMaxMsgs, DefaultMaxMsgs)
	if err != nil {
		return serviceLimits{}, err
	}
	fetchConcurrency, err := envconfig.PositiveInt(
		getenv,
		EnvFetchConcurrency,
		DefaultFetchConcurrency,
	)
	if err != nil {
		return serviceLimits{}, err
	}
	runPageBudget, err := envconfig.PositiveInt(getenv, EnvRunPageBudget, DefaultRunPageBudget)
	if err != nil {
		return serviceLimits{}, err
	}
	frontierCap, err := envconfig.PositiveInt(getenv, EnvFrontierCap, DefaultFrontierCap)
	if err != nil {
		return serviceLimits{}, err
	}
	maxBodyBytes, err := envconfig.PositiveInt64(getenv, EnvMaxBodyBytes, DefaultMaxBodyBytes)
	if err != nil {
		return serviceLimits{}, err
	}
	fetchDeadline, err := envconfig.Duration(getenv, EnvFetchDeadline, DefaultFetchDeadline)
	if err != nil {
		return serviceLimits{}, err
	}
	indexEnabled, err := envconfig.Bool(getenv, EnvIndexOutputEnabled, true)
	if err != nil {
		return serviceLimits{}, err
	}
	pageEnabled, err := envconfig.Bool(getenv, EnvPageOutputEnabled, false)
	if err != nil {
		return serviceLimits{}, err
	}
	if !indexEnabled && !pageEnabled {
		return serviceLimits{}, fmt.Errorf(
			"at least one of %s or %s must be enabled",
			EnvIndexOutputEnabled, EnvPageOutputEnabled,
		)
	}

	return serviceLimits{
		pageIndexMaxMsgs: pageIndexMaxMsgs,
		pagesMaxMsgs:     pagesMaxMsgs,
		fetchConcurrency: fetchConcurrency,
		runPageBudget:    runPageBudget,
		frontierCap:      frontierCap,
		maxBodyBytes:     maxBodyBytes,
		fetchDeadline:    fetchDeadline,
		indexEnabled:     indexEnabled,
		pageEnabled:      pageEnabled,
	}, nil
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	natsURL := strings.TrimSpace(getenv(EnvNATSURL))
	if natsURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvNATSURL)
	}
	proxyURL, err := requiredURL(getenv, EnvProxyURL)
	if err != nil {
		return ServiceConfig{}, err
	}
	proxyDialMode, err := proxyDialModeFromEnv(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}
	limits, err := loadServiceLimits(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		NATSURL:            natsURL,
		OrdersSubject:      envconfig.String(getenv, EnvOrdersSubject, DefaultOrdersSubject),
		OrdersDurable:      envconfig.String(getenv, EnvOrdersDurable, DefaultOrdersDurable),
		PageIndexSubject:   envconfig.String(getenv, EnvPageIndexSubject, DefaultPageIndexSubject),
		PageIndexMaxMsgs:   limits.pageIndexMaxMsgs,
		PagesSubject:       envconfig.String(getenv, EnvPagesSubject, DefaultPagesSubject),
		PagesMaxMsgs:       limits.pagesMaxMsgs,
		ProxyURL:           proxyURL,
		ProxyDialMode:      proxyDialMode,
		FetchConcurrency:   limits.fetchConcurrency,
		IndexOutputEnabled: limits.indexEnabled,
		PageOutputEnabled:  limits.pageEnabled,
		RunPageBudget:      limits.runPageBudget,
		FrontierCap:        limits.frontierCap,
		MaxBodyBytes:       limits.maxBodyBytes,
		FetchDeadline:      limits.fetchDeadline,
		ContentTypes:       mediaTypes(getenv, EnvContentTypes),
		OpsAddr:            envconfig.String(getenv, EnvOpsAddr, DefaultOpsAddr),
		UserAgent:          envconfig.String(getenv, EnvUserAgent, DefaultUserAgent),
	}, nil
}

func requiredURL(getenv func(string) string, key string) (*url.URL, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return nil, fmt.Errorf("%s: must be set", key)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s: scheme must be http or https", key)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%s: must include a host", key)
	}
	return parsed, nil
}

func proxyDialModeFromEnv(getenv func(string) string) (httpfetch.ProxyDialMode, error) {
	name := envconfig.String(getenv, EnvProxyDialMode, DefaultProxyDialMode)
	mode, ok := proxyDialModeByName[name]
	if !ok {
		return 0, fmt.Errorf("%s: unknown proxy dial mode %q", EnvProxyDialMode, name)
	}
	return mode, nil
}

func mediaTypes(getenv func(string) string, key string) []string {
	values := envconfig.List(getenv, key)
	for i, value := range values {
		values[i] = strings.ToLower(value)
	}
	return values
}
