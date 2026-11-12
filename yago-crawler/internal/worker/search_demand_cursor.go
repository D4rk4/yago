package worker

import "strconv"

func searchDemandCursorRegressed(current, next string) bool {
	currentSequence, currentErr := strconv.ParseUint(current, 10, 64)
	nextSequence, nextErr := strconv.ParseUint(next, 10, 64)
	return currentErr == nil && nextErr == nil && nextSequence < currentSequence
}
