package worker

import "time"

func mergeSearchDemandCandidates(
	left searchDemandCandidate,
	right searchDemandCandidate,
) searchDemandCandidate {
	selected := left
	if right.score > left.score {
		selected = right
	}
	selected.focusedAt = latestTime(left.focusedAt, right.focusedAt)

	return selected
}

func searchDemandRecrawlMinimumAge(
	candidate searchDemandCandidate,
	now time.Time,
) time.Duration {
	minimumAge := searchDemandRecrawlInterval
	if candidate.focusedAt.IsZero() || candidate.focusedAt.After(now) {
		return minimumAge
	}

	return min(minimumAge, now.Sub(candidate.focusedAt))
}
