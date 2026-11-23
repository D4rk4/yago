package searchindex

const relaxedPassageQuorumCeiling = 3

func relaxedPassageQuorum(requirements int, identifiers int) int {
	return min(
		relaxedRequirementMinimum(requirements),
		max(relaxedPassageQuorumCeiling, identifiers),
	)
}
