package searchcore

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

func resultSiteIdentity(rawHost string) string {
	host := normalizedResultHost(rawHost)
	site, _ := registrableResultSiteIdentity(host)

	return site
}

func normalizedResultHost(rawHost string) string {
	host := strings.ToLower(strings.TrimSpace(rawHost))

	return strings.TrimSuffix(host, ".")
}

func registrableResultSiteIdentity(host string) (string, bool) {
	if host == "" {
		return "", false
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host, false
	}

	return site, true
}

func observedRegistrableParentSite(
	host string,
	siteAppearances map[string]resultSiteAppearance,
) string {
	if !resultHostCanHaveRegistrableParent(host) {
		return ""
	}
	parent := host
	for {
		separator := strings.IndexByte(parent, '.')
		if separator < 0 {
			return ""
		}
		parent = parent[separator+1:]
		if appearance, ok := siteAppearances[parent]; ok && appearance.registrable {
			return parent
		}
	}
}

func resultHostCanHaveRegistrableParent(host string) bool {
	switch {
	case host == "":
		return false
	case strings.HasPrefix(host, "."):
		return false
	case strings.Contains(host, ".."):
		return false
	case strings.ContainsAny(host, ":[]"):
		return false
	}
	for _, character := range host {
		if character >= 'a' && character <= 'z' || character >= 0x80 {
			return true
		}
	}

	return false
}
