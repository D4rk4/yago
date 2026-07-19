package yagonode

import (
	"fmt"
	"net/netip"
	"sort"
)

const (
	envEgressAllowCIDRs                      = "YAGO_EGRESS_ALLOW_CIDRS"
	maximumEgressAllowCIDRConfigurationBytes = 8 << 10
	maximumEgressAllowCIDRs                  = 64
)

// parseEgressAllowCIDRs reads a comma-separated list of private CIDRs the egress
// guard should admit even when private networks are otherwise blocked. An empty
// value yields no allowlist, keeping the default private-network denial.
func parseEgressAllowCIDRs(raw string) ([]netip.Prefix, error) {
	if len(raw) > maximumEgressAllowCIDRConfigurationBytes {
		return nil, fmt.Errorf(
			"CIDR list must contain at most %d bytes",
			maximumEgressAllowCIDRConfigurationBytes,
		)
	}
	items := splitList(raw)
	if len(items) > maximumEgressAllowCIDRs {
		return nil, fmt.Errorf("CIDR list must contain at most %d entries", maximumEgressAllowCIDRs)
	}
	prefixes := make([]netip.Prefix, 0, len(items))
	seen := make(map[netip.Prefix]struct{}, len(items))
	for _, item := range items {
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("parse cidr %q: %w", item, err)
		}
		prefix = prefix.Masked()
		if _, duplicate := seen[prefix]; duplicate {
			continue
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(left, right int) bool {
		return prefixes[left].String() < prefixes[right].String()
	})
	if len(prefixes) == 0 {
		return nil, nil
	}

	return prefixes, nil
}
