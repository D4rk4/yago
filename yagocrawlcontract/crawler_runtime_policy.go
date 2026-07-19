package yagocrawlcontract

import (
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	DefaultCrawlerBrowserFailureThreshold = 5
	DefaultCrawlerMaximumDepth            = 5
	DefaultCrawlerMaximumHostConcurrency  = 2
	DefaultCrawlerRunPagesPerMinute       = 30
	DefaultCrawlerSitemapURLLimit         = 10_000
	MaximumCrawlerAllowedPrivateCIDRs     = 64
	MaximumCrawlerBrowserFailureThreshold = 1_000
	MaximumCrawlerMaximumDepth            = 64
	MaximumCrawlerMaximumHostConcurrency  = MaximumFetchWorkerConcurrency
	MaximumCrawlerRunPagesPerMinute       = 1_000_000
	MaximumCrawlerSitemapURLLimit         = 1_000_000
	MaximumCrawlerUserAgentBytes          = 256
	MaximumCrawlerBrowserPathBytes        = 4_096
	MaximumCrawlerMetricsAddressBytes     = 261
)

const (
	DefaultCrawlerConnectTimeout  = 5 * time.Second
	DefaultCrawlerCrawlDelay      = time.Second
	DefaultCrawlerHeaderTimeout   = 10 * time.Second
	DefaultCrawlerRequestTimeout  = 15 * time.Second
	DefaultCrawlerShutdownGrace   = 10 * time.Second
	DefaultCrawlerTLSTimeout      = 5 * time.Second
	MaximumCrawlerCrawlDelay      = time.Hour
	MaximumCrawlerPhaseTimeout    = 2 * time.Minute
	MaximumCrawlerRequestTimeout  = 10 * time.Minute
	MaximumCrawlerShutdownGrace   = 5 * time.Minute
	MinimumCrawlerPositiveTimeout = time.Millisecond
)

var crawlerPrivateNetworkRoots = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("fc00::/7"),
}

type CrawlerRuntimePolicy struct {
	AllowPrivateNetworks    bool
	AllowedPrivateCIDRs     []netip.Prefix
	BrowserFailureThreshold int
	BrowserPath             string
	BrowserSandbox          bool
	ConnectTimeout          time.Duration
	CrawlDelay              time.Duration
	HeaderTimeout           time.Duration
	MaximumDepth            int
	MaximumHostConcurrency  int
	MetricsAddress          string
	RequestTimeout          time.Duration
	RunPagesPerMinute       uint32
	SitemapURLLimit         int
	TLSTimeout              time.Duration
	ShutdownGrace           time.Duration
	UserAgent               string
}

func DefaultCrawlerRuntimePolicy() CrawlerRuntimePolicy {
	return CrawlerRuntimePolicy{
		BrowserFailureThreshold: DefaultCrawlerBrowserFailureThreshold,
		ConnectTimeout:          DefaultCrawlerConnectTimeout,
		CrawlDelay:              DefaultCrawlerCrawlDelay,
		HeaderTimeout:           DefaultCrawlerHeaderTimeout,
		MaximumDepth:            DefaultCrawlerMaximumDepth,
		MaximumHostConcurrency:  DefaultCrawlerMaximumHostConcurrency,
		RequestTimeout:          DefaultCrawlerRequestTimeout,
		RunPagesPerMinute:       DefaultCrawlerRunPagesPerMinute,
		SitemapURLLimit:         DefaultCrawlerSitemapURLLimit,
		TLSTimeout:              DefaultCrawlerTLSTimeout,
		ShutdownGrace:           DefaultCrawlerShutdownGrace,
		UserAgent:               "yago-crawler (+https://github.com/D4rk4/yago/)",
	}
}

func ParseCrawlerPrivateCIDRs(raw string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0)
	seen := make(map[netip.Prefix]struct{})
	for _, item := range strings.Split(raw, ",") {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("parse private CIDR %q: %w", value, err)
		}
		prefix = prefix.Masked()
		if !crawlerPrivatePrefixAllowed(prefix) {
			return nil, fmt.Errorf(
				"CIDR %s is not contained by an RFC1918 or IPv6 ULA network",
				prefix,
			)
		}
		if _, duplicate := seen[prefix]; duplicate {
			continue
		}
		if len(prefixes) == MaximumCrawlerAllowedPrivateCIDRs {
			return nil, fmt.Errorf(
				"private CIDR list must contain at most %d entries",
				MaximumCrawlerAllowedPrivateCIDRs,
			)
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(left, right int) bool {
		return prefixes[left].String() < prefixes[right].String()
	})
	if len(prefixes) == 0 {
		return nil, nil
	}

	return prefixes, nil
}

func FormatCrawlerPrivateCIDRs(prefixes []netip.Prefix) string {
	values := make([]string, len(prefixes))
	for index, prefix := range prefixes {
		values[index] = prefix.Masked().String()
	}
	sort.Strings(values)

	return strings.Join(values, ",")
}

func (policy CrawlerRuntimePolicy) Equal(other CrawlerRuntimePolicy) bool {
	return policy.AllowPrivateNetworks == other.AllowPrivateNetworks &&
		FormatCrawlerPrivateCIDRs(policy.AllowedPrivateCIDRs) ==
			FormatCrawlerPrivateCIDRs(other.AllowedPrivateCIDRs) &&
		policy.BrowserFailureThreshold == other.BrowserFailureThreshold &&
		policy.BrowserPath == other.BrowserPath &&
		policy.BrowserSandbox == other.BrowserSandbox &&
		policy.ConnectTimeout == other.ConnectTimeout &&
		policy.CrawlDelay == other.CrawlDelay &&
		policy.HeaderTimeout == other.HeaderTimeout &&
		policy.MaximumDepth == other.MaximumDepth &&
		policy.MaximumHostConcurrency == other.MaximumHostConcurrency &&
		policy.MetricsAddress == other.MetricsAddress &&
		policy.RequestTimeout == other.RequestTimeout &&
		policy.RunPagesPerMinute == other.RunPagesPerMinute &&
		policy.SitemapURLLimit == other.SitemapURLLimit &&
		policy.TLSTimeout == other.TLSTimeout &&
		policy.ShutdownGrace == other.ShutdownGrace &&
		policy.UserAgent == other.UserAgent
}

func crawlerPrivatePrefixAllowed(prefix netip.Prefix) bool {
	return slices.ContainsFunc(crawlerPrivateNetworkRoots, func(root netip.Prefix) bool {
		return prefix.Addr().BitLen() == root.Addr().BitLen() &&
			prefix.Bits() >= root.Bits() && root.Contains(prefix.Addr())
	})
}
