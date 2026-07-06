package yagonode

import (
	"fmt"
	"net/url"
	"strings"
)

// normalizePublicBaseURL validates the operator-configured public origin used
// behind a reverse proxy: empty means "derive from each request", anything
// else must be an absolute http(s) URL without userinfo; a trailing slash is
// dropped so templates concatenate cleanly.
func normalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid public base url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.User != nil {
		return "", fmt.Errorf("public base url must be absolute http(s) without credentials")
	}

	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host+parsed.Path, "/"), nil
}
