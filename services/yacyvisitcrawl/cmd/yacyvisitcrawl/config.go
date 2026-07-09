package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/envconfig"
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

	orderTimeout, err := envconfig.Duration(getenv, EnvOrderTimeout, DefaultOrderTimeout)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxInFlight, err := envconfig.PositiveInt(getenv, EnvMaxInFlight, DefaultMaxInFlight)
	if err != nil {
		return ServiceConfig{}, err
	}
	maxBodyBytes, err := envconfig.PositiveInt64(getenv, EnvMaxBodyBytes, DefaultMaxBodyBytes)
	if err != nil {
		return ServiceConfig{}, err
	}
	profile, err := crawlProfile(getenv)
	if err != nil {
		return ServiceConfig{}, err
	}

	return ServiceConfig{
		NATSURL:       natsURL,
		OrdersSubject: envconfig.String(getenv, EnvOrdersSubject, DefaultOrdersSubject),
		ListenAddr:    envconfig.String(getenv, EnvListenAddr, DefaultListenAddr),
		OpsAddr:       envconfig.String(getenv, EnvOpsAddr, DefaultOpsAddr),
		OrderTimeout:  orderTimeout,
		MaxInFlight:   maxInFlight,
		MaxBodyBytes:  maxBodyBytes,
		CrawlProfile:  profile,
	}, nil
}

func crawlProfile(getenv func(string) string) (yacycrawlcontract.CrawlProfile, error) {
	scopeName := envconfig.String(getenv, EnvCrawlScope, DefaultCrawlScope)
	scope, ok := crawlScopeByName[scopeName]
	if !ok {
		return yacycrawlcontract.CrawlProfile{}, fmt.Errorf(
			"%s: unknown crawl scope %q", EnvCrawlScope, scopeName,
		)
	}

	maxDepth, err := envconfig.NonNegativeInt(getenv, EnvCrawlMaxDepth, DefaultCrawlMaxDepth)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}
	maxPagesPerHost, err := envconfig.Int(
		getenv,
		EnvCrawlMaxPagesPerHost,
		DefaultCrawlMaxPagesPerHost,
	)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}
	if maxPagesPerHost != yacycrawlcontract.UnlimitedPagesPerHost && maxPagesPerHost <= 0 {
		return yacycrawlcontract.CrawlProfile{}, fmt.Errorf(
			"%s: must be positive or %d for unlimited",
			EnvCrawlMaxPagesPerHost, yacycrawlcontract.UnlimitedPagesPerHost,
		)
	}
	delay, err := envconfig.NonNegativeDuration(getenv, EnvCrawlDelay)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}
	allowQueryURLs, err := envconfig.Bool(getenv, EnvCrawlAllowQueryURLs, false)
	if err != nil {
		return yacycrawlcontract.CrawlProfile{}, err
	}

	return yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Name:            envconfig.String(getenv, EnvCrawlName, ""),
		Scope:           scope,
		URLMustMatch:    matchOrAll(envconfig.String(getenv, EnvCrawlURLMustMatch, "")),
		URLMustNotMatch: envconfig.String(getenv, EnvCrawlURLMustNotMatch, ""),
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
