package docidentity

import (
	"net/url"
	"sort"
	"strings"
)

func CanonicalizeURL(raw string, trackingParams []string) (string, bool) {
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
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = canonicalizePath(parsed.Path)
	parsed.RawQuery = canonicalizeQuery(parsed.Query(), trackingParams)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), true
}

func canonicalizePath(path string) string {
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func canonicalizeQuery(values url.Values, trackingParams []string) string {
	for _, tracking := range trackingParams {
		values.Del(tracking)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var built strings.Builder
	for i, key := range keys {
		sort.Strings(values[key])
		for j, value := range values[key] {
			if i > 0 || j > 0 {
				built.WriteByte('&')
			}
			built.WriteString(url.QueryEscape(key))
			built.WriteByte('=')
			built.WriteString(url.QueryEscape(value))
		}
	}
	return built.String()
}
