package yagonode

import "math"

func crawlTallyTotal(current, observed uint64) uint64 {
	if observed > math.MaxUint64-current {
		return math.MaxUint64
	}

	return current + observed
}
