package yagonode

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func seedStatistic(value yagomodel.Optional[int]) (int, bool) {
	parsed, known := value.Get()

	return parsed, known && parsed >= 0
}

func seedTransferStatistic(value yagomodel.Optional[int64]) (int64, bool) {
	parsed, known := value.Get()

	return parsed, known && parsed >= 0
}

func seedAgeStatistic(seed yagomodel.Seed, now time.Time) (int, bool) {
	birth, known := seed.BirthDate.Get()
	if !known || birth.Time().After(now) {
		return 0, false
	}

	return int(now.UTC().Sub(birth.Time()) / (24 * time.Hour)), true
}

func seedLastSeenStatistic(seed yagomodel.Seed, now time.Time) (time.Time, bool) {
	lastSeen, known := seed.LastSeen.Get()
	if !known || lastSeen.Time().After(now) {
		return time.Time{}, false
	}

	return lastSeen.Time(), true
}
