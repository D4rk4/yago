package weburl

import "net/url"

func RobotsURL(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}
	robots := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/robots.txt"}
	return robots.String(), true
}
