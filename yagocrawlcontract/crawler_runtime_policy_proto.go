package yagocrawlcontract

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func CrawlerRuntimePolicyToProto(
	policy CrawlerRuntimePolicy,
) (*crawlrpc.CrawlerRuntimePolicy, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	prefixes := make([]string, len(policy.AllowedPrivateCIDRs))
	for index, prefix := range policy.AllowedPrivateCIDRs {
		prefixes[index] = prefix.Masked().String()
	}

	browserSandbox := policy.BrowserSandbox
	browserPath := policy.BrowserPath
	metricsAddress := policy.MetricsAddress
	frontierStateMaximumBytes := policy.FrontierStateMaximumBytes

	return &crawlrpc.CrawlerRuntimePolicy{
		AllowPrivateNetworks:       policy.AllowPrivateNetworks,
		AllowedPrivateCidrs:        prefixes,
		BrowserFailureThreshold:    crawlerRuntimePolicyUint32(policy.BrowserFailureThreshold),
		BrowserPath:                &browserPath,
		BrowserSandbox:             &browserSandbox,
		ConnectTimeoutMilliseconds: crawlerRuntimePolicyMilliseconds(policy.ConnectTimeout),
		CrawlDelayMilliseconds:     crawlerRuntimePolicyMilliseconds(policy.CrawlDelay),
		HeaderTimeoutMilliseconds:  crawlerRuntimePolicyMilliseconds(policy.HeaderTimeout),
		MaximumDepth:               crawlerRuntimePolicyUint32(policy.MaximumDepth),
		MaximumHostConcurrency:     crawlerRuntimePolicyUint32(policy.MaximumHostConcurrency),
		MetricsAddress:             &metricsAddress,
		FrontierStateMaxBytes:      &frontierStateMaximumBytes,
		RequestTimeoutMilliseconds: crawlerRuntimePolicyMilliseconds(policy.RequestTimeout),
		RunPagesPerMinute:          policy.RunPagesPerMinute,
		SitemapUrlLimit:            crawlerRuntimePolicyUint32(policy.SitemapURLLimit),
		TlsTimeoutMilliseconds:     crawlerRuntimePolicyMilliseconds(policy.TLSTimeout),
		ShutdownGraceMilliseconds:  crawlerRuntimePolicyMilliseconds(policy.ShutdownGrace),
		UserAgent:                  policy.UserAgent,
	}, nil
}

func CrawlerRuntimePolicyFromProto(
	message *crawlrpc.CrawlerRuntimePolicy,
) (CrawlerRuntimePolicy, error) {
	return CrawlerRuntimePolicyFromProtoWithFallback(
		message,
		DefaultCrawlerRuntimePolicy(),
	)
}

func CrawlerRuntimePolicyFromProtoWithFallback(
	message *crawlrpc.CrawlerRuntimePolicy,
	fallback CrawlerRuntimePolicy,
) (CrawlerRuntimePolicy, error) {
	if message == nil {
		return CrawlerRuntimePolicy{}, fmt.Errorf("crawler runtime policy is missing")
	}
	prefixes, err := ParseCrawlerPrivateCIDRs(strings.Join(message.GetAllowedPrivateCidrs(), ","))
	if err != nil {
		return CrawlerRuntimePolicy{}, err
	}
	durations, err := crawlerRuntimePolicyDurationsFromProto(message)
	if err != nil {
		return CrawlerRuntimePolicy{}, err
	}
	browserSandbox := fallback.BrowserSandbox
	if message.BrowserSandbox != nil {
		browserSandbox = message.GetBrowserSandbox()
	}
	browserPath := fallback.BrowserPath
	if message.BrowserPath != nil {
		browserPath = message.GetBrowserPath()
	}
	metricsAddress := fallback.MetricsAddress
	if message.MetricsAddress != nil {
		metricsAddress = message.GetMetricsAddress()
	}
	frontierStateMaximumBytes := fallback.FrontierStateMaximumBytes
	if message.FrontierStateMaxBytes != nil {
		frontierStateMaximumBytes = message.GetFrontierStateMaxBytes()
	}
	policy := CrawlerRuntimePolicy{
		AllowPrivateNetworks:      message.GetAllowPrivateNetworks(),
		AllowedPrivateCIDRs:       prefixes,
		BrowserFailureThreshold:   int(message.GetBrowserFailureThreshold()),
		BrowserPath:               browserPath,
		BrowserSandbox:            browserSandbox,
		ConnectTimeout:            durations.connectTimeout,
		CrawlDelay:                durations.crawlDelay,
		FrontierStateMaximumBytes: frontierStateMaximumBytes,
		HeaderTimeout:             durations.headerTimeout,
		MaximumDepth:              int(message.GetMaximumDepth()),
		MaximumHostConcurrency:    int(message.GetMaximumHostConcurrency()),
		MetricsAddress:            metricsAddress,
		RequestTimeout:            durations.requestTimeout,
		RunPagesPerMinute:         message.GetRunPagesPerMinute(),
		SitemapURLLimit:           int(message.GetSitemapUrlLimit()),
		TLSTimeout:                durations.tlsTimeout,
		ShutdownGrace:             durations.shutdownGrace,
		UserAgent:                 message.GetUserAgent(),
	}
	if err := policy.Validate(); err != nil {
		return CrawlerRuntimePolicy{}, err
	}

	return policy, nil
}

type crawlerRuntimePolicyDurations struct {
	connectTimeout time.Duration
	crawlDelay     time.Duration
	headerTimeout  time.Duration
	requestTimeout time.Duration
	tlsTimeout     time.Duration
	shutdownGrace  time.Duration
}

func crawlerRuntimePolicyDurationsFromProto(
	message *crawlrpc.CrawlerRuntimePolicy,
) (crawlerRuntimePolicyDurations, error) {
	durations := crawlerRuntimePolicyDurations{}
	fields := []struct {
		name      string
		raw       uint64
		maximum   time.Duration
		allowZero bool
		target    *time.Duration
	}{
		{
			name: "connect timeout", raw: message.GetConnectTimeoutMilliseconds(),
			maximum: MaximumCrawlerPhaseTimeout, target: &durations.connectTimeout,
		},
		{
			name: "crawl delay", raw: message.GetCrawlDelayMilliseconds(),
			maximum: MaximumCrawlerCrawlDelay, allowZero: true, target: &durations.crawlDelay,
		},
		{
			name: "header timeout", raw: message.GetHeaderTimeoutMilliseconds(),
			maximum: MaximumCrawlerPhaseTimeout, target: &durations.headerTimeout,
		},
		{
			name: "request timeout", raw: message.GetRequestTimeoutMilliseconds(),
			maximum: MaximumCrawlerRequestTimeout, target: &durations.requestTimeout,
		},
		{
			name: "TLS timeout", raw: message.GetTlsTimeoutMilliseconds(),
			maximum: MaximumCrawlerPhaseTimeout, target: &durations.tlsTimeout,
		},
		{
			name: "shutdown grace", raw: message.GetShutdownGraceMilliseconds(),
			maximum: MaximumCrawlerShutdownGrace, target: &durations.shutdownGrace,
		},
	}
	for _, field := range fields {
		value, err := crawlerRuntimePolicyDurationFromMilliseconds(
			field.name,
			field.raw,
			field.maximum,
			field.allowZero,
		)
		if err != nil {
			return crawlerRuntimePolicyDurations{}, err
		}
		*field.target = value
	}

	return durations, nil
}

func crawlerRuntimePolicyDurationFromMilliseconds(
	name string,
	raw uint64,
	maximum time.Duration,
	allowZero bool,
) (time.Duration, error) {
	milliseconds, err := strconv.ParseInt(strconv.FormatUint(raw, 10), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s milliseconds exceed the supported range", name)
	}
	if milliseconds > maximum.Milliseconds() || (!allowZero && milliseconds == 0) {
		return 0, fmt.Errorf("%s milliseconds are outside the supported range", name)
	}

	return time.Duration(milliseconds) * time.Millisecond, nil
}

func crawlerRuntimePolicyUint32(value int) uint32 {
	parsed, _ := strconv.ParseUint(strconv.Itoa(value), 10, 32)

	return uint32(parsed)
}

func crawlerRuntimePolicyMilliseconds(value time.Duration) uint64 {
	milliseconds, _ := strconv.ParseUint(strconv.FormatInt(value.Milliseconds(), 10), 10, 64)

	return milliseconds
}
