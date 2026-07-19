package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func applyCrawlerAccessBootstrap(
	getenv func(string) string,
	policy *yagocrawlcontract.CrawlerRuntimePolicy,
) error {
	var err error
	policy.AllowPrivateNetworks, err = boolEnv(
		getenv,
		envCrawlerAllowPrivateNetworks,
		policy.AllowPrivateNetworks,
	)
	if err != nil {
		return err
	}
	policy.BrowserSandbox, err = boolEnv(
		getenv,
		envCrawlerBrowserSandbox,
		policy.BrowserSandbox,
	)
	if err != nil {
		return err
	}
	policy.AllowedPrivateCIDRs, err = yagocrawlcontract.ParseCrawlerPrivateCIDRs(
		getenv(envCrawlerAllowCIDRs),
	)
	if err != nil {
		return fmt.Errorf("%s: %w", envCrawlerAllowCIDRs, err)
	}
	if userAgent := strings.TrimSpace(getenv(envCrawlerUserAgent)); userAgent != "" {
		policy.UserAgent = userAgent
	}

	return nil
}
