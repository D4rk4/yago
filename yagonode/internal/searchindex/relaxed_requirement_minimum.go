package searchindex

import "math"

func relaxedRequirementMinimum(requirements int) int {
	if requirements < 3 {
		return requirements
	}
	minimum := int(math.Ceil(float64(requirements) * 0.6))

	return max(1, min(requirements-1, minimum))
}
