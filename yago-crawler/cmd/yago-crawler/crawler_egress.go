package main

import (
	"fmt"
	"net/netip"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const EnvEgressAllowCIDRs = "YAGO_CRAWLER_ALLOW_CIDRS"

// parseEgressAllowCIDRs reads a comma-separated list of private CIDRs the crawler
// egress guard admits even when private networks are otherwise blocked, so an
// intranet crawl can reach named internal ranges without opening all of RFC 1918
// and unique-local space. An empty value keeps the default private denial.
func parseEgressAllowCIDRs(raw string) ([]netip.Prefix, error) {
	prefixes, err := yagocrawlcontract.ParseCrawlerPrivateCIDRs(raw)
	if err != nil {
		return nil, fmt.Errorf("parse crawler egress CIDRs: %w", err)
	}

	return prefixes, nil
}
