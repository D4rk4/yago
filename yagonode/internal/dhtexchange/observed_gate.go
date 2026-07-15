package dhtexchange

func observedGate(
	name GateName,
	known bool,
	open bool,
	closedReason string,
	unknownReason string,
) GateResult {
	if !known {
		return GateResult{Name: name, Reason: unknownReason}
	}

	return gate(name, open, closedReason)
}
