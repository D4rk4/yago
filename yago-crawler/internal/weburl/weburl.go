package weburl

import (
	"net/url"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// Normalize canonicalizes an http(s) URL to the one spelling the frontier
// visited-set, document keys, and recrawl schedule share, so tracking-parameter
// and session-id variants of one page stop burning crawl budget and index
// space as distinct URLs (RFC 3986 normalization; Manku et al. WWW 2007
// motivate dedup before fetch).
//
// The rules live in yagocrawlcontract so the node seeds crawl orders under the
// same spelling this crawler stores them under; a second copy here would let
// the two drift and re-crawl the same page forever.
func Normalize(raw string) (string, bool) {
	return yagocrawlcontract.CanonicalURL(raw)
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
