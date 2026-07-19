package yagonode

import "net/netip"

func normalizeEgressAllowCIDRs(raw string) (string, error) {
	prefixes, err := parseEgressAllowCIDRs(raw)
	if err != nil {
		return "", err
	}

	return formatPrefixes(prefixes), nil
}

func egressAllowCIDRsFromCanonical(raw string) []netip.Prefix {
	items := splitList(raw)
	prefixes := make([]netip.Prefix, len(items))
	for index, item := range items {
		prefixes[index] = netip.MustParsePrefix(item)
	}

	return prefixes
}
