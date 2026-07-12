package rankingtrain

import "time"

func deterministicProposalConfig(revision string, family ModelFamily) Config {
	config := DefaultConfig(revision, family)
	config.MeasurementClock = fixedIntervalMeasurementClock()

	return config
}

func fixedIntervalMeasurementClock() func() time.Time {
	current := time.Unix(0, 0)

	return func() time.Time {
		current = current.Add(time.Millisecond)

		return current
	}
}
