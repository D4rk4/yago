package crawlscope

import (
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func NormalizeSeed(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func HostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func (c CompiledProfile) AdmitLinks(baseRawURL string, links []string) []string {
	base, err := url.Parse(baseRawURL)
	if err != nil {
		return nil
	}
	admitted := make([]string, 0, len(links))
	for _, link := range links {
		if normalized, ok := c.admit(base, link); ok {
			admitted = append(admitted, normalized)
		}
	}
	return admitted
}

func (c CompiledProfile) admit(base *url.URL, link string) (string, bool) {
	ref, err := url.Parse(link)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}
	if !scopeAllows(c.Profile.Scope, base, resolved) {
		return "", false
	}
	if !c.Profile.AllowQueryURLs && resolved.RawQuery != "" {
		return "", false
	}
	if !c.URLAllowed(resolved.String()) {
		return "", false
	}
	return NormalizeSeed(resolved.String())
}

func scopeAllows(scope yacycrawlcontract.CrawlScope, base, resolved *url.URL) bool {
	switch scope {
	case yacycrawlcontract.ScopeWide:
		return true
	case yacycrawlcontract.ScopeSubpath:
		return resolved.Host == base.Host && strings.HasPrefix(resolved.Path, basePath(base.Path))
	default:
		return resolved.Host == base.Host
	}
}

func basePath(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx+1]
	}
	return path
}
