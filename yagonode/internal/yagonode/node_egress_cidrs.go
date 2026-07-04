package yagonode

import (
	"fmt"
	"net/netip"
)

const envEgressAllowCIDRs = "YACY_EGRESS_ALLOW_CIDRS"

// parseEgressAllowCIDRs reads a comma-separated list of private CIDRs the egress
// guard should admit even when private networks are otherwise blocked. An empty
// value yields no allowlist, keeping the default private-network denial.
func parseEgressAllowCIDRs(raw string) ([]netip.Prefix, error) {
	items := splitList(raw)
	prefixes := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("parse cidr %q: %w", item, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	if len(prefixes) == 0 {
		return nil, nil
	}

	return prefixes, nil
}
