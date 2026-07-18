package tavilyapi

import (
	"net/url"
	"strings"
)

func parseCrawlBaseURL(raw string) (*url.URL, error) {
	value := strings.TrimSpace(raw)
	if value != "" && !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" ||
		parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, badRequest("url must be absolute http(s) or a host name")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}

	return parsed, nil
}
