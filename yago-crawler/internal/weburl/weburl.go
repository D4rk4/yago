package weburl

import "net/url"

// Normalize canonicalizes an http(s) URL to the one spelling the frontier
// visited-set, document keys, and recrawl schedule share, so tracking-parameter
// and session-id variants of one page stop burning crawl budget and index
// space as distinct URLs (RFC 3986 normalization; Manku et al. WWW 2007
// motivate dedup before fetch).
func Normalize(raw string) (string, bool) {
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
	canonicalizeURL(parsed)

	return parsed.String(), true
}

func Host(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func ParseBase(rawURL string) (*url.URL, bool) {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, false
	}
	return base, true
}

func Resolve(base *url.URL, link string) (*url.URL, bool) {
	ref, err := url.Parse(link)
	if err != nil {
		return nil, false
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return nil, false
	}
	return resolved, true
}
