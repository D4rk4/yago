package adminui

import (
	"fmt"
	"math"
)

func crawlPopulationAtLeast(minimum uint64, members ...uint64) bool {
	remaining := minimum
	for _, member := range members {
		if member >= remaining {
			return true
		}
		remaining -= member
	}

	return minimum == 0
}

func crawlPopulationShare(part uint64, members ...uint64) float64 {
	population := 0.0
	for _, member := range members {
		population += float64(member)
	}
	if population == 0 {
		return 0
	}

	share := float64(part) / population
	return min(1, max(0, share))
}

func crawlPopulationPercent(part uint64, members ...uint64) string {
	share := crawlPopulationShare(part, members...)
	if part == 0 {
		return "0%"
	}
	if crawlPopulationEquals(part, members...) {
		return "100%"
	}
	if share < 0.01 {
		return "<1%"
	}
	if share > 0.99 {
		return ">99%"
	}

	return fmt.Sprintf("%.0f%%", 100*share)
}

func crawlPopulationEquals(part uint64, members ...uint64) bool {
	population := uint64(0)
	for _, member := range members {
		if member > math.MaxUint64-population {
			return false
		}
		population += member
	}

	return part == population
}
