package main

import (
	"fmt"
	"net/netip"
	"strings"
)

const EnvEgressAllowCIDRs = "YACYCRAWLER_ALLOW_CIDRS"

// parseEgressAllowCIDRs reads a comma-separated list of private CIDRs the crawler
// egress guard admits even when private networks are otherwise blocked, so an
// intranet crawl can reach named internal ranges without opening all of RFC 1918
// and unique-local space. An empty value keeps the default private denial.
func parseEgressAllowCIDRs(raw string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0)
	for _, item := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(trimmed)
		if err != nil {
			return nil, fmt.Errorf("parse cidr %q: %w", trimmed, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	if len(prefixes) == 0 {
		return nil, nil
	}

	return prefixes, nil
}
