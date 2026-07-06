package websearch

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// bingRedirectVersionPrefix tags the encoding version of a Bing click-tracking
// u parameter; today's SERPs emit "a1" followed by the unpadded base64url of
// the destination URL.
const bingRedirectVersionPrefix = "a1"

// decodeBingRedirect resolves a Bing /ck/a click-tracking href to the page it
// points at, so the node surfaces, seeds, and indexes real destinations rather
// than www.bing.com tracking URLs. Any URL that is not a Bing redirect passes
// through unchanged; a redirect whose destination cannot be recovered yields ""
// so the caller drops the hit instead of publishing a tracking link.
func decodeBingRedirect(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.ToLower(parsed.Hostname())
	if (host != "bing.com" && !strings.HasSuffix(host, ".bing.com")) ||
		!strings.HasPrefix(parsed.EscapedPath(), "/ck/") {
		return rawURL
	}
	packed := parsed.Query().Get("u")
	if !strings.HasPrefix(packed, bingRedirectVersionPrefix) {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(packed, bingRedirectVersionPrefix),
	)
	if err != nil {
		return ""
	}
	target, err := url.Parse(string(decoded))
	if err != nil || !target.IsAbs() || target.Hostname() == "" {
		return ""
	}

	return target.String()
}
