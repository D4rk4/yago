package chromedpfetch

import (
	"strings"

	"github.com/chromedp/chromedp"
)

func proxyExecAllocatorOptions(environment func(string) string) []chromedp.ExecAllocatorOption {
	server := proxyServer(
		proxyEnvironmentValue(environment, "HTTP_PROXY", "http_proxy"),
		proxyEnvironmentValue(environment, "HTTPS_PROXY", "https_proxy"),
	)
	if server == "" {
		return nil
	}
	options := []chromedp.ExecAllocatorOption{chromedp.ProxyServer(server)}
	if bypass := proxyBypass(
		proxyEnvironmentValue(environment, "NO_PROXY", "no_proxy"),
	); bypass != "" {
		options = append(options, chromedp.Flag("proxy-bypass-list", bypass))
	}
	return options
}

func proxyServer(httpProxy, httpsProxy string) string {
	var schemes []string
	if httpProxy != "" {
		schemes = append(schemes, "http="+httpProxy)
	}
	if httpsProxy != "" {
		schemes = append(schemes, "https="+httpsProxy)
	}
	return strings.Join(schemes, ";")
}

func proxyBypass(noProxy string) string {
	var hosts []string
	for _, host := range strings.Split(noProxy, ",") {
		if trimmed := strings.TrimSpace(host); trimmed != "" {
			hosts = append(hosts, trimmed)
		}
	}
	return strings.Join(hosts, ";")
}

func proxyEnvironmentValue(environment func(string) string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(environment(name)); value != "" {
			return value
		}
	}
	return ""
}
