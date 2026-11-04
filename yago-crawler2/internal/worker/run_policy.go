package worker

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"golang.org/x/net/publicsuffix"
)

type scopeRoot struct {
	url        *url.URL
	domain     string
	pathPrefix string
}

type runRules struct {
	profile           yagocrawlcontract.CrawlProfile
	roots             []scopeRoot
	urlMustMatch      *regexp.Regexp
	urlMustNotMatch   *regexp.Regexp
	indexMustMatch    *regexp.Regexp
	indexMustNotMatch *regexp.Regexp
	maximumDepth      int
	maximumPages      uint64
	maximumHostPages  int
}

func compileRunRules(
	order yagocrawlcontract.CrawlOrder,
	policy yagocrawlcontract.CrawlerRuntimePolicy,
) (runRules, error) {
	profile := order.Profile
	rules := runRules{profile: profile, maximumDepth: min(profile.MaxDepth, policy.MaximumDepth)}
	if profile.MaxDepth < 0 || profile.MaxPagesPerHost < yagocrawlcontract.UnlimitedPagesPerHost {
		return runRules{}, fmt.Errorf("crawl profile has invalid limits")
	}
	var err error
	if rules.urlMustMatch, err = compileProfilePattern(profile.URLMustMatch); err != nil {
		return runRules{}, fmt.Errorf("compile crawl URL inclusion rule: %w", err)
	}
	if rules.urlMustNotMatch, err = compileProfilePattern(profile.URLMustNotMatch); err != nil {
		return runRules{}, fmt.Errorf("compile crawl URL exclusion rule: %w", err)
	}
	if rules.indexMustMatch, err = compileProfilePattern(profile.IndexURLMustMatch); err != nil {
		return runRules{}, fmt.Errorf("compile indexing URL inclusion rule: %w", err)
	}
	if rules.indexMustNotMatch, err = compileProfilePattern(profile.IndexURLMustNotMatch); err != nil {
		return runRules{}, fmt.Errorf("compile indexing URL exclusion rule: %w", err)
	}
	maximumPages := profile.EffectiveMaxPagesPerRun(yagocrawlcontract.DefaultMaxPagesPerRun)
	if maximumPages <= 0 {
		maximumPages = yagocrawlcontract.DefaultMaxPagesPerRun
	}
	rules.maximumPages = uint64(maximumPages)
	if profile.MaxPagesPerHost > 0 {
		rules.maximumHostPages = profile.MaxPagesPerHost
	}
	for _, limit := range policy.AutomaticDiscoveryLimits {
		if !strings.EqualFold(limit.ProfileName, profile.Name) {
			continue
		}
		rules.maximumDepth = min(rules.maximumDepth, int(limit.MaximumDepth))
		if limit.MaximumPagesPerHost > 0 &&
			(rules.maximumHostPages == 0 || limit.MaximumPagesPerHost < rules.maximumHostPages) {
			rules.maximumHostPages = limit.MaximumPagesPerHost
		}
		if limit.MaximumPagesPerRun > 0 && uint64(limit.MaximumPagesPerRun) < rules.maximumPages {
			rules.maximumPages = uint64(limit.MaximumPagesPerRun)
		}
	}
	for _, request := range order.Requests {
		mode, valid := yagocrawlcontract.NormalizeCrawlRequestMode(request.Mode)
		if !valid {
			return runRules{}, fmt.Errorf("crawl order contains an unsupported request mode")
		}
		if mode == yagocrawlcontract.CrawlRequestModeRobots {
			continue
		}
		canonical, valid := yagocrawlcontract.CanonicalURL(request.URL)
		if !valid {
			return runRules{}, fmt.Errorf("crawl order contains an invalid seed URL")
		}
		parsed, err := url.Parse(canonical)
		if err != nil || parsed.Hostname() == "" {
			return runRules{}, fmt.Errorf("crawl order contains an invalid seed host")
		}
		domain, err := publicsuffix.EffectiveTLDPlusOne(parsed.Hostname())
		if err != nil {
			domain = strings.ToLower(parsed.Hostname())
		}
		pathPrefix := parsed.EscapedPath()
		if pathPrefix == "" {
			pathPrefix = "/"
		}
		rules.roots = append(rules.roots, scopeRoot{url: parsed, domain: domain, pathPrefix: pathPrefix})
	}
	return rules, nil
}

func compileProfilePattern(raw string) (*regexp.Regexp, error) {
	if raw == "" {
		return nil, nil
	}
	return regexp.Compile(raw)
}

func (rules runRules) allowsCrawlURL(rawURL string) bool {
	canonical, ok := yagocrawlcontract.CanonicalURL(rawURL)
	if !ok {
		return false
	}
	parsed, err := url.Parse(canonical)
	if err != nil || parsed.User != nil ||
		!matchesInclusionPattern(rules.urlMustMatch, canonical) ||
		matchesExclusionPattern(rules.urlMustNotMatch, canonical) {
		return false
	}
	if !rules.profile.AllowQueryURLs && parsed.RawQuery != "" {
		return false
	}
	switch rules.profile.Scope {
	case yagocrawlcontract.ScopeWide:
		return true
	case yagocrawlcontract.ScopeDomain:
		domain, domainErr := publicsuffix.EffectiveTLDPlusOne(parsed.Hostname())
		if domainErr != nil {
			domain = strings.ToLower(parsed.Hostname())
		}
		for _, root := range rules.roots {
			if strings.EqualFold(domain, root.domain) {
				return true
			}
		}
		return false
	case yagocrawlcontract.ScopeSubpath:
		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}
		for _, root := range rules.roots {
			if strings.EqualFold(parsed.Host, root.url.Host) &&
				(root.pathPrefix == "/" || path == root.pathPrefix ||
					strings.HasPrefix(path, strings.TrimRight(root.pathPrefix, "/")+"/")) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (rules runRules) allowsIndexing(rawURL string, pageNoindex bool, canonicalURL string) bool {
	if pageNoindex || !matchesInclusionPattern(rules.indexMustMatch, rawURL) ||
		matchesExclusionPattern(rules.indexMustNotMatch, rawURL) {
		return false
	}
	if rules.profile.NoindexCanonicalMismatch && canonicalURL != "" {
		canonical, valid := yagocrawlcontract.CanonicalURL(canonicalURL)
		normalized, normalizedValid := yagocrawlcontract.CanonicalURL(rawURL)
		if valid && normalizedValid && canonical != normalized {
			return false
		}
	}
	return true
}

func matchesInclusionPattern(pattern *regexp.Regexp, value string) bool {
	return pattern == nil || pattern.MatchString(value)
}

func matchesExclusionPattern(pattern *regexp.Regexp, value string) bool {
	return pattern != nil && pattern.MatchString(value)
}
