package crawladmission

import (
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

func (c AdmissionProfile) AdmitLinks(baseRawURL string, links []string) []string {
	base, ok := weburl.ParseBase(baseRawURL)
	if !ok {
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

func (c AdmissionProfile) admit(base *url.URL, link string) (string, bool) {
	resolved, ok := weburl.Resolve(base, link)
	if !ok {
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
	return weburl.Normalize(resolved.String())
}

func scopeAllows(scope yagocrawlcontract.CrawlScope, base, resolved *url.URL) bool {
	switch scope {
	case yagocrawlcontract.ScopeWide:
		return true
	case yagocrawlcontract.ScopeSubpath:
		return resolved.Host == base.Host && strings.HasPrefix(resolved.Path, basePath(base.Path))
	default:
		return sameDomainHost(resolved.Host, base.Host)
	}
}

// sameDomainHost reports whether two hosts belong to the same crawl domain. YaCy
// 1.4 relaxed the site operator so a bare domain and its www. variant count as
// one: a domain-scoped crawl of anticisco.ru follows links into www.anticisco.ru
// and back. Comparison is case-insensitive per DNS; only a single leading www.
// label is dropped, and any port is preserved.
func sameDomainHost(a, b string) bool {
	return domainKey(a) == domainKey(b)
}

func domainKey(host string) string {
	host = strings.ToLower(host)
	if trimmed := strings.TrimPrefix(host, "www."); trimmed != host {
		return trimmed
	}
	return host
}

func basePath(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx+1]
	}
	return path
}
