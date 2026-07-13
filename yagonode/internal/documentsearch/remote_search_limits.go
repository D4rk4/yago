package documentsearch

import "time"

const (
	remoteSearchMaximumCount = 10
	remoteSearchMaximumTime  = 3 * time.Second
	msgRemoteSearchDeadline  = "inbound remote search deadline reached"
)

func receiverSearchCount(requested int) int {
	if requested <= 0 || requested > remoteSearchMaximumCount {
		return remoteSearchMaximumCount
	}

	return requested
}

func receiverSearchTime(requestedMilliseconds int) time.Duration {
	maximumMilliseconds := int(remoteSearchMaximumTime / time.Millisecond)
	if requestedMilliseconds <= 0 || requestedMilliseconds > maximumMilliseconds {
		return remoteSearchMaximumTime
	}

	return time.Duration(requestedMilliseconds) * time.Millisecond
}
