package searchcore

func pseudoRelevanceActivationLimit(limit int) int {
	if limit <= 0 {
		limit = DefaultPublicLimit
	}

	return min(limit, prfActivateBelow)
}
