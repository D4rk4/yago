package crawlorder

import "github.com/D4rk4/yago/yagocrawlcontract"

type recoveredSessionManifest struct {
	seenFrame            bool
	declared             bool
	recoveryPrefixClosed bool
	remaining            map[string]struct{}
}

func (m *recoveredSessionManifest) acceptFrame(
	batchStarted bool,
	batchLeaseIDs []string,
	sessionLeaseIDs []string,
) ([]string, error) {
	if m.recoveryPrefixClosed ||
		batchStarted && m.declared && m.seenFrame && len(m.remaining) == 0 {
		return nil, errRecoveredOrderBatchFraming
	}
	if len(sessionLeaseIDs) > 0 {
		if m.seenFrame || !batchStarted ||
			len(sessionLeaseIDs) > yagocrawlcontract.MaximumHeartbeatActiveLeases ||
			!validRecoveredLeaseHeader(sessionLeaseIDs) {
			return nil, errRecoveredOrderBatchFraming
		}
		m.declared = true
		m.remaining = make(map[string]struct{}, len(sessionLeaseIDs))
		for _, leaseID := range sessionLeaseIDs {
			m.remaining[leaseID] = struct{}{}
		}
	}
	if batchStarted && m.declared {
		for _, leaseID := range batchLeaseIDs {
			if _, found := m.remaining[leaseID]; !found {
				return nil, errRecoveredOrderBatchFraming
			}
		}
		for _, leaseID := range batchLeaseIDs {
			delete(m.remaining, leaseID)
		}
	}
	m.seenFrame = true
	if len(sessionLeaseIDs) > 0 {
		return sessionLeaseIDs, nil
	}

	return batchLeaseIDs, nil
}

func (m recoveredSessionManifest) pending() bool {
	return m.declared && len(m.remaining) > 0
}

func (m *recoveredSessionManifest) acceptOrdinary() bool {
	if m.pending() {
		return false
	}
	m.recoveryPrefixClosed = true

	return true
}
