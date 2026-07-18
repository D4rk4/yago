package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

func normalizedHeartbeatLeaseIDs(leaseIDs []string) ([]string, bool) {
	if len(leaseIDs) > yagocrawlcontract.MaximumHeartbeatActiveLeases {
		return nil, false
	}
	seen := make(map[string]struct{}, len(leaseIDs))
	normalized := make([]string, 0, len(leaseIDs))
	for _, leaseID := range leaseIDs {
		if !yagocrawlcontract.ValidCrawlLeaseID(leaseID) {
			return nil, false
		}
		if _, duplicate := seen[leaseID]; duplicate {
			continue
		}
		seen[leaseID] = struct{}{}
		normalized = append(normalized, leaseID)
	}

	return normalized, true
}

func normalizedHeartbeatDirectiveAcknowledgments(
	directiveIDs []uint64,
) ([]uint64, bool) {
	if len(directiveIDs) > yagocrawlcontract.MaximumHeartbeatDirectiveAcknowledgments {
		return nil, false
	}
	seen := make(map[uint64]struct{}, len(directiveIDs))
	normalized := make([]uint64, 0, len(directiveIDs))
	for _, directiveID := range directiveIDs {
		if directiveID == 0 {
			return nil, false
		}
		if _, duplicate := seen[directiveID]; duplicate {
			continue
		}
		seen[directiveID] = struct{}{}
		normalized = append(normalized, directiveID)
	}

	return normalized, true
}
