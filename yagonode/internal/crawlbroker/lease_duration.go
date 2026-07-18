package crawlbroker

import "time"

func durationMilliseconds(duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}
	milliseconds := duration / time.Millisecond
	if milliseconds == 0 {
		return 1
	}

	return uint64(milliseconds)
}
