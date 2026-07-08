package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

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

	pageIndexMaxMsgs, err := envPositiveInt64(getenv, EnvPageIndexMaxMsgs, DefaultMaxMsgs)
	if err != nil {
		return ServiceConfig{}, err
	}
	pagesMaxMsgs, err := envPositiveInt64(getenv, EnvPagesMaxMsgs, DefaultMaxMsgs)
	if err != nil {
		return ServiceConfig{}, err
	}
	fetchConcurrency, err := envPositiveInt(getenv, EnvFetchConcurrency, DefaultFetchConcurrency)
	if err != nil {
		return ServiceConfig{}, err
	}
	runPageBudget, err := envPositiveInt(getenv, EnvRunPageBudget, DefaultRunPageBudget)
	if err != nil {
		return ServiceConfig{}, err
	}
	frontierCap, err := envPositiveInt(getenv, EnvFrontierCap, DefaultFrontierCap)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxBodyBytes, err := envPositiveInt64(getenv, EnvMaxBodyBytes, DefaultMaxBodyBytes)
	if err != nil {
		return ServiceConfig{}, err
	}
	fetchDeadline, err := envDuration(getenv, EnvFetchDeadline, DefaultFetchDeadline)
	if err != nil {
		return ServiceConfig{}, err
	}
	indexEnabled, err := envBool(getenv, EnvIndexOutputEnabled, true)
	if err != nil {
		return ServiceConfig{}, err
	}
	pageEnabled, err := envBool(getenv, EnvPageOutputEnabled, false)
	if err != nil {
		return ServiceConfig{}, err
	}
	if !indexEnabled && !pageEnabled {
		return ServiceConfig{}, fmt.Errorf(
			"at least one of %s or %s must be enabled",
			EnvIndexOutputEnabled, EnvPageOutputEnabled,
		)
	}

	return ServiceConfig{
		NATSURL:            natsURL,
		OrdersSubject:      envString(getenv, EnvOrdersSubject, DefaultOrdersSubject),
		OrdersDurable:      envString(getenv, EnvOrdersDurable, DefaultOrdersDurable),
		PageIndexSubject:   envString(getenv, EnvPageIndexSubject, DefaultPageIndexSubject),
		PageIndexMaxMsgs:   pageIndexMaxMsgs,
		PagesSubject:       envString(getenv, EnvPagesSubject, DefaultPagesSubject),
		PagesMaxMsgs:       pagesMaxMsgs,
		ProxyURL:           proxyURL,
		ProxyDialMode:      proxyDialMode,
		FetchConcurrency:   fetchConcurrency,
		IndexOutputEnabled: indexEnabled,
		PageOutputEnabled:  pageEnabled,
		RunPageBudget:      runPageBudget,
		FrontierCap:        frontierCap,
		MaxBodyBytes:       maxBodyBytes,
		FetchDeadline:      fetchDeadline,
		ContentTypes:       envList(getenv, EnvContentTypes),
		OpsAddr:            envString(getenv, EnvOpsAddr, DefaultOpsAddr),
		UserAgent:          envString(getenv, EnvUserAgent, DefaultUserAgent),
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
	name := envString(getenv, EnvProxyDialMode, DefaultProxyDialMode)
	mode, ok := proxyDialModeByName[name]
	if !ok {
		return 0, fmt.Errorf("%s: unknown proxy dial mode %q", EnvProxyDialMode, name)
	}
	return mode, nil
}

func envList(getenv func(string) string, key string) []string {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return nil
	}
	var values []string
	for item := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			values = append(values, strings.ToLower(trimmed))
		}
	}
	return values
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
	if value <= 0 {
		return 0, fmt.Errorf("%s: must be positive", key)
	}
	return value, nil
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
