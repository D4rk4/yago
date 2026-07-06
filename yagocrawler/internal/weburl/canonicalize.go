package weburl

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// trackingParams are query parameters that only ever carry ad-click or
// analytics identity, never page content — the intersection of the Brave,
// Firefox strip-on-share, and AdGuard unconditional strip lists — plus the
// classic session-id parameters production crawlers (Nutch, Heritrix) drop by
// default. Ambiguous names (ref, source, bare id) are deliberately absent:
// stripping a semantic parameter would alias distinct pages.
var trackingParams = map[string]bool{
	"gclid": true, "dclid": true, "gbraid": true, "wbraid": true,
	"fbclid": true, "msclkid": true,
	"yclid": true, "ysclid": true, "ymclid": true, "_openstat": true,
	"twclid": true, "ttclid": true, "irclickid": true,
	"s_cid": true, "mc_eid": true, "_ga": true, "_gl": true,
	"oly_anon_id": true, "oly_enc_id": true,
	"vero_id": true, "vero_conv": true,
	"__hsfp": true, "__hssc": true, "__hstc": true, "_hsenc": true,
	"hsctatracking": true,
	"phpsessid":     true, "jsessionid": true, "sessionid": true,
	"cfid": true, "cftoken": true,
}

// sessionValuePattern is Heritrix's guard for the ambiguous "sid" parameter:
// it is stripped only when its value looks like a session token (32
// hexadecimal or alphanumeric characters), never when it could be a semantic
// id such as a story or section number.
var sessionValuePattern = regexp.MustCompile(`^[0-9a-zA-Z]{32}$`)

// canonicalizeURL applies RFC 3986 semantics-preserving normalization plus
// production-crawler parameter hygiene, so the visited-set, document keys, and
// recrawl schedule all see one spelling of one page: lowercased host, default
// ports elided, dot-segments removed, an empty path spelled "/", query
// parameters sorted with tracking and session identifiers stripped. Path and
// query casing is untouched and trailing slashes are preserved (Google treats
// /fish and /fish/ as different URLs).
func canonicalizeURL(parsed *url.URL) {
	parsed.Host = canonicalHost(parsed)
	parsed.Path = canonicalPath(parsed.Path)
	parsed.RawPath = ""
	parsed.RawQuery = canonicalQuery(parsed.RawQuery)
	parsed.Fragment = ""
	parsed.RawFragment = ""
}

func canonicalHost(parsed *url.URL) string {
	host := strings.ToLower(parsed.Host)
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") ||
		(parsed.Scheme == "https" && port == "443") {
		host = strings.TrimSuffix(host, ":"+port)
	}

	return host
}

func canonicalPath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}
	cleaned := path.Clean(rawPath)
	if strings.HasSuffix(rawPath, "/") && cleaned != "/" {
		cleaned += "/"
	}

	return cleaned
}

// canonicalQuery strips tracking and session parameters and sorts the rest
// alphabetically (url.Values.Encode sorts keys), so parameter order never
// aliases one page into many. A query Go cannot parse is kept verbatim rather
// than half-rewritten.
func canonicalQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	for key := range values {
		if dropQueryParam(key, values.Get(key)) {
			delete(values, key)
		}
	}

	return values.Encode()
}

func dropQueryParam(key, value string) bool {
	folded := strings.ToLower(key)
	if trackingParams[folded] ||
		strings.HasPrefix(folded, "utm_") ||
		strings.HasPrefix(folded, "aspsessionid") {
		return true
	}

	return folded == "sid" && sessionValuePattern.MatchString(value)
}
