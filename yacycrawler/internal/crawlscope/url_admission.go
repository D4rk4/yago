package crawlscope

import (
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/weburl"
)

func (c CompiledProfile) AdmitLinks(baseRawURL string, links []string) []string {
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

func (c CompiledProfile) admit(base *url.URL, link string) (string, bool) {
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
