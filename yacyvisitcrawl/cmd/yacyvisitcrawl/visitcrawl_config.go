package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	EnvNATSURL       = "NATS_URL"
	EnvOrdersSubject = "NATS_ORDERS_SUBJECT"

	EnvListenAddr   = "YACYVISITCRAWL_LISTEN_ADDR"
	EnvOpsAddr      = "YACYVISITCRAWL_OPS_ADDR"
	EnvOrderTimeout = "YACYVISITCRAWL_ORDER_TIMEOUT"
	EnvMaxInFlight  = "YACYVISITCRAWL_MAX_IN_FLIGHT"
	EnvMaxBodyBytes = "YACYVISITCRAWL_MAX_BODY_BYTES"

	EnvCrawlScope           = "YACYVISITCRAWL_CRAWL_SCOPE"
	EnvCrawlName            = "YACYVISITCRAWL_CRAWL_NAME"
	EnvCrawlMaxDepth        = "YACYVISITCRAWL_CRAWL_MAX_DEPTH"
	EnvCrawlURLMustMatch    = "YACYVISITCRAWL_CRAWL_URL_MUST_MATCH"
	EnvCrawlURLMustNotMatch = "YACYVISITCRAWL_CRAWL_URL_MUST_NOT_MATCH"
	EnvCrawlMaxPagesPerHost = "YACYVISITCRAWL_CRAWL_MAX_PAGES_PER_HOST"
	EnvCrawlDelay           = "YACYVISITCRAWL_CRAWL_DELAY"
	EnvCrawlAllowQueryURLs  = "YACYVISITCRAWL_CRAWL_ALLOW_QUERY_URLS"

	DefaultOrdersSubject        = "yacy.crawl.orders"
	DefaultListenAddr           = ":8091"
	DefaultOpsAddr              = ":9091"
	DefaultOrderTimeout         = 5 * time.Second
	DefaultMaxInFlight          = 256
	DefaultMaxBodyBytes         = 4 << 10
	DefaultCrawlScope           = "domain"
	DefaultCrawlMaxDepth        = 1
	DefaultCrawlMaxPagesPerHost = 100
)

type ServiceConfig struct {
	NATSURL       string
	OrdersSubject string
	ListenAddr    string
	OpsAddr       string
	OrderTimeout  time.Duration
	MaxInFlight   int
	MaxBodyBytes  int64
	CrawlProfile  yacycrawlcontract.CrawlProfile
}

var crawlScopeByName = map[string]yacycrawlcontract.CrawlScope{
	"domain":  yacycrawlcontract.ScopeDomain,
	"wide":    yacycrawlcontract.ScopeWide,
	"subpath": yacycrawlcontract.ScopeSubpath,
}

func (c ServiceConfig) OrdersStreamSpec() yacycrawlcontract.OrdersStreamSpec {
	return yacycrawlcontract.OrdersStreamSpec{Subject: c.OrdersSubject}
}

func LoadServiceConfig(getenv func(string) string) (ServiceConfig, error) {
	natsURL := strings.TrimSpace(getenv(EnvNATSURL))
	if natsURL == "" {
		return ServiceConfig{}, fmt.Errorf("%s: must be set", EnvNATSURL)
	}

	orderTimeout, err := envPositiveDuration(getenv, EnvOrderTimeout, DefaultOrderTimeout)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxInFlight, err := envPositiveInt(getenv, EnvMaxInFlight, DefaultMaxInFlight)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxBodyBytes, err := envPositiveInt64(getenv, EnvMaxBodyBytes, DefaultMaxBodyBytes)
	if err != nil {
		return ServiceConfig{}, err
	}
	profile, err := crawlProfile(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		NATSURL:       natsURL,
		OrdersSubject: envString(getenv, EnvOrdersSubject, DefaultOrdersSubject),
		ListenAddr:    envString(getenv, EnvListenAddr, DefaultListenAddr),
		OpsAddr:       envString(getenv, EnvOpsAddr, DefaultOpsAddr),
		OrderTimeout:  orderTimeout,
		MaxInFlight:   maxInFlight,
		MaxBodyBytes:  maxBodyBytes,
		CrawlProfile:  profile,
	}, nil
}

func crawlProfile(getenv func(string) string) (yacycrawlcontract.CrawlProfile, error) {
	scopeName := envString(getenv, EnvCrawlScope, DefaultCrawlScope)
	scope, ok := crawlScopeByName[scopeName]
	if !ok {
		return yacycrawlcontract.CrawlProfile{}, fmt.Errorf(
			"%s: unknown crawl scope %q", EnvCrawlScope, scopeName,
		)
	}

	maxDepth, err := envNonNegativeInt(getenv, EnvCrawlMaxDepth, DefaultCrawlMaxDepth)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}
	maxPagesPerHost, err := envInt(getenv, EnvCrawlMaxPagesPerHost, DefaultCrawlMaxPagesPerHost)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}
	if maxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost && maxPagesPerHost <= 0 {
		return yacycrawlcontract.CrawlProfile{}, fmt.Errorf(
			"%s: must be positive or %d for unlimited",
			EnvCrawlMaxPagesPerHost, yacycrawlcontract.UnlimitedPagesPerHost,
		)
	}
	delay, err := envNonNegativeDuration(getenv, EnvCrawlDelay)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}
	allowQueryURLs, err := envBool(getenv, EnvCrawlAllowQueryURLs, false)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}

	return yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Name:            envString(getenv, EnvCrawlName, ""),
		Scope:           scope,
		URLMustMatch:    matchOrAll(envString(getenv, EnvCrawlURLMustMatch, "")),
		URLMustNotMatch: envString(getenv, EnvCrawlURLMustNotMatch, ""),
		MaxDepth:        maxDepth,
		AllowQueryURLs:  allowQueryURLs,
		MaxPagesPerHost: maxPagesPerHost,
		CrawlDelay:      delay,
	}), nil
}

func matchOrAll(pattern string) string {
	if pattern == "" {
		return yacycrawlcontract.MatchAll
	}
	return pattern
}

func envString(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func envNonNegativeInt(getenv func(string) string, key string, fallback int) (int, error) {
	value, err := envInt(getenv, key, fallback)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("%s: must not be negative", key)
	}
	return value, nil
}

func envPositiveInt(getenv func(string) string, key string, fallback int) (int, error) {
	value, err := envInt(getenv, key, fallback)
	if err != nil {
		return 0, err
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

func envNonNegativeDuration(getenv func(string) string, key string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return 0, nil
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
